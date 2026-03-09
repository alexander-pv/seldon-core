/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	copy2 "github.com/otiai10/copy"
	log "github.com/sirupsen/logrus"

	"github.com/seldonio/seldon-core/apis/go/v2/mlops/scheduler"

	"github.com/seldonio/seldon-core/scheduler/v2/pkg/agent/filemanager"
	"github.com/seldonio/seldon-core/scheduler/v2/pkg/util"
)

// V2ModelRepositoryLogicalLayout uses logical model name and generation for paths:
// repoPath/logicalName/generation/ (e.g. models/model-a/2/). Only one version per model
// is kept; previous version dirs are removed when a new generation is copied.
type V2ModelRepositoryLogicalLayout struct {
	logger                       log.FieldLogger
	fileManager                  filemanager.FileManager
	repoPath                     string
	modelRepositoryHandler       ModelRepositoryHandler
	envoyHost                    string
	envoyPort                    int
	maxBackoffRetryModelDownload time.Duration
}

// NewModelRepositoryLogicalLayout creates a repository that uses logical naming and version folders.
func NewModelRepositoryLogicalLayout(logger log.FieldLogger,
	fileManager filemanager.FileManager,
	repoPath string,
	modelRepositoryHandler ModelRepositoryHandler,
	envoyHost string,
	envoyPort int,
	maxBackoffRetryModelDownload time.Duration,
) *V2ModelRepositoryLogicalLayout {
	return &V2ModelRepositoryLogicalLayout{
		logger:                       logger.WithField("Name", "V2ModelRepositoryLogicalLayout"),
		fileManager:                  fileManager,
		repoPath:                     repoPath,
		modelRepositoryHandler:       modelRepositoryHandler,
		envoyHost:                    envoyHost,
		envoyPort:                    envoyPort,
		maxBackoffRetryModelDownload: maxBackoffRetryModelDownload,
	}
}

func (r *V2ModelRepositoryLogicalLayout) GetModelRuntimeInfo(modelName string) (*scheduler.ModelRuntimeInfo, error) {
	logicalName, _, err := util.GetOrignalModelNameAndVersion(modelName)
	if err != nil {
		logicalName = modelName
	}
	modelPathInRepo := filepath.Join(r.repoPath, logicalName)
	return r.modelRepositoryHandler.GetModelRuntimeInfo(modelPathInRepo)
}

func (r *V2ModelRepositoryLogicalLayout) DownloadModelVersion(
	ctx context.Context,
	modelName string,
	version uint32,
	generation uint32,
	modelSpec *scheduler.ModelSpec,
	config []byte,
) (modelVersionFolderPtr *string, err error) {
	logger := r.logger.WithField("func", "DownloadModelVersion")

	logicalName, _, err := util.GetOrignalModelNameAndVersion(modelName)
	if err != nil {
		return nil, fmt.Errorf("logical layout requires versioned model name: %w", err)
	}

	artifactVersion := modelSpec.ArtifactVersion
	srcUri := modelSpec.Uri

	logger.Debugf("running with model %s (logical %s) generation %d srcUri %s", modelName, logicalName, generation, srcUri)

	var rclonePath string
	err = backoff.RetryNotify(func() error {
		var err error
		rclonePath, err = r.fileManager.Copy(ctx, modelName, srcUri, config)
		if err != nil {
			var urlError *url.Error
			if errors.As(err, &urlError) {
				return err
			}
			logger.WithError(err).Warn("Got permanent error, not retrying to download model")
			return backoff.Permanent(err)
		}
		return nil
	}, backoff.NewExponentialBackOff(backoff.WithMaxElapsedTime(r.maxBackoffRetryModelDownload)), func(err error, t time.Duration) {
		logger.WithError(err).Warnf("Failed to download model, retrying in %s", t)
	})
	if err != nil {
		return nil, err
	}

	defer func() {
		if err_purge := r.fileManager.PurgeLocal(rclonePath); err_purge != nil {
			err = errors.Join(err, err_purge)
		}
	}()

	modelVersionFolder, foundVersionFolder, err := r.modelRepositoryHandler.FindModelVersionFolder(
		modelName,
		artifactVersion,
		rclonePath,
	)
	if err != nil {
		return nil, err
	}

	logger.Debugf("Found model %s (logical %s) generation %d at %s", modelName, logicalName, generation, modelVersionFolder)

	modelPathInRepo := filepath.Join(r.repoPath, logicalName)
	if err := os.MkdirAll(modelPathInRepo, os.ModePerm); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(modelPathInRepo)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			versionDir := filepath.Join(modelPathInRepo, e.Name())
			if versionDir != "" {
				_ = os.RemoveAll(versionDir)
			}
		}
	}

	versionStr := fmt.Sprintf("%d", generation)
	modelVersionPathInRepo := filepath.Join(modelPathInRepo, versionStr)
	opt := copy2.Options{
		OnDirExists: func(sr, dst string) copy2.DirExistsAction { return copy2.Replace },
		Sync:        true,
	}
	if err := copy2.Copy(modelVersionFolder, modelVersionPathInRepo, opt); err != nil {
		return nil, err
	}

	if err := r.modelRepositoryHandler.UpdateModelVersion(
		logicalName,
		generation,
		modelVersionPathInRepo,
		modelSpec,
	); err != nil {
		return nil, err
	}

	if explainerSpec := modelSpec.GetExplainer(); explainerSpec != nil {
		if err := r.modelRepositoryHandler.SetExplainer(modelVersionPathInRepo, explainerSpec, r.envoyHost, r.envoyPort); err != nil {
			return nil, err
		}
	}
	if llmSpec := modelSpec.GetLlm(); llmSpec != nil {
		if err := r.modelRepositoryHandler.SetLlm(modelVersionPathInRepo, llmSpec, r.envoyHost, r.envoyPort); err != nil {
			return nil, err
		}
	}
	if err := r.modelRepositoryHandler.SetExtraParameters(modelVersionPathInRepo, modelSpec.GetParameters()); err != nil {
		return nil, err
	}

	if err := r.modelRepositoryHandler.UpdateModelRepository(
		logicalName,
		modelVersionFolder,
		foundVersionFolder,
		modelPathInRepo,
	); err != nil {
		return nil, err
	}

	return &modelVersionFolder, nil
}

func (r *V2ModelRepositoryLogicalLayout) RemoveModelVersion(modelName string) error {
	modelPath, err := r.versionPathToRemove(modelName)
	if err != nil {
		return err
	}
	r.logger.Debugf("Removing model version path: %s", modelPath)
	if err := os.RemoveAll(modelPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r *V2ModelRepositoryLogicalLayout) versionPathToRemove(modelName string) (string, error) {
	logicalName, version, err := util.GetOrignalModelNameAndVersion(modelName)
	if err != nil {
		logicalName = modelName
		version = 0
	}
	var path string
	if version != 0 {
		path = filepath.Join(r.repoPath, logicalName, strconv.FormatUint(uint64(version), 10))
	} else {
		path = filepath.Join(r.repoPath, logicalName)
	}
	rel, relErr := filepath.Rel(r.repoPath, path)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing to remove path outside repository: %s", path)
	}
	return path, nil
}

func (r *V2ModelRepositoryLogicalLayout) Ready() error {
	return r.fileManager.Ready()
}
