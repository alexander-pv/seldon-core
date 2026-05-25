/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

// Package tritonv2 provides a Triton repository handler that uses logical model names
// and version folders (model_a/1, model_a/2). The repository layer copies artifacts into
// modelRepoPath/generation/; this handler only writes config.pbtxt at model level.
package tritonv2

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	copy2 "github.com/otiai10/copy"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/seldonio/seldon-core/apis/go/v2/mlops/scheduler"

	pb "github.com/seldonio/seldon-core/scheduler/v2/pkg/agent/repository/triton/config"
	"github.com/seldonio/seldon-core/scheduler/v2/pkg/util"
)

const (
	TritonConfigFile = "config.pbtxt"
)

type TritonV2RepositoryHandler struct {
	logger log.FieldLogger
}

func NewTritonV2RepositoryHandler(logger log.FieldLogger) *TritonV2RepositoryHandler {
	return &TritonV2RepositoryHandler{logger: logger.WithField("name", "TritonV2RepositoryHandler")}
}

func copyNonConfigFilesToModelRepo(src string, dst string) error {
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Compute relative path to preserve structure
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			// Skip directories named 1–99999 - Triton model versions
			if path != src {
				if n, err := strconv.Atoi(info.Name()); err == nil && n >= 1 && n <= 99999 {
					return filepath.SkipDir
				}
			}
			// Otherwise create directory
			return os.MkdirAll(targetPath, info.Mode())
		}

		// Copy non-config.pbtxt files
		if filepath.Base(path) != TritonConfigFile {
			err := copy2.Copy(path, targetPath)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// UpdateModelRepository writes config.pbtxt at model level only. Artifacts are already
// in modelRepoPath/generation/ (copied by the logical-layout repository).
func (t *TritonV2RepositoryHandler) UpdateModelRepository(modelName string, rclonePath string, isVersionFolder bool, modelRepoPath string) error {
	configFilePathRepo := filepath.Join(modelRepoPath, TritonConfigFile)
	var configFilePath string
	if isVersionFolder {
		t.logger.Infof("Copy files from versioned folder %s to %s", filepath.Dir(rclonePath), modelRepoPath)
		// copy all non-config.pbtxt files from folder above version to repo folder
		err := copyNonConfigFilesToModelRepo(filepath.Dir(rclonePath), modelRepoPath)
		if err != nil {
			return err
		}
		// look for config.pbtxt in folder above of current folder if this is a version folder
		configFilePath = filepath.Join(filepath.Dir(rclonePath), TritonConfigFile)
		if _, err := os.Stat(configFilePath); err != nil {
			return t.createConfigFileWithName(modelName, configFilePathRepo)
		}
	} else {
		t.logger.Infof("Copy files from non-versioned folder %s to %s", rclonePath, modelRepoPath)
		// copy all non-config.pbtxt files from folder to repo folder
		err := copyNonConfigFilesToModelRepo(rclonePath, modelRepoPath)
		if err != nil {
			return err
		}
		// look for config.pbtxt in same folder as model artifacts
		configFilePath = filepath.Join(rclonePath, TritonConfigFile)
		if _, err := os.Stat(configFilePath); err != nil {
			return t.createConfigFileWithName(modelName, configFilePathRepo)
		}
	}
	err := copy2.Copy(configFilePath, configFilePathRepo)
	if err != nil {
		return err
	}
	return t.updateModelNameInConfig(modelName, configFilePathRepo)
}

func saveConfigFile(path string, config *pb.ModelConfig) error {
	data, err := prototext.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, fs.ModePerm)
}

func (t *TritonV2RepositoryHandler) createConfigFileWithName(modelName string, path string) error {
	config := pb.ModelConfig{}
	config.Name = modelName
	return saveConfigFile(path, &config)
}

// updateModelNameInConfig sets the model name to the logical name (no revision/version suffix),
// clears any instance_group names so Triton forms them from the logical name, and normalizes
// ensemble step model_name fields to logical names. Ensures config.pbtxt does not contain versioned model names.
func (t *TritonV2RepositoryHandler) updateModelNameInConfig(modelName string, path string) error {
	s, err := t.loadConfigFromFile(path)
	if err != nil {
		return err
	}
	s.Name = modelName
	for _, ig := range s.InstanceGroup {
		if ig != nil {
			ig.Name = ""
		}
	}
	if ens := s.GetEnsembleScheduling(); ens != nil {
		for _, step := range ens.Step {
			if step != nil && step.ModelName != "" {
				if logical, _, err := util.GetOrignalModelNameAndVersion(step.ModelName); err == nil {
					step.ModelName = logical
				}
			}
		}
	}
	return saveConfigFile(path, s)
}

func (t *TritonV2RepositoryHandler) FindModelVersionFolder(_ string, version *uint32, path string) (string, bool, error) {
	if version != nil {
		return t.findModelVersionInPath(path, *version)
	} else {
		return t.findHighestVersionInPath(path)
	}
}

func (t *TritonV2RepositoryHandler) UpdateModelVersion(_ string, _ uint32, _ string, _ *scheduler.ModelSpec) error {
	return nil
}

func (t *TritonV2RepositoryHandler) findModelVersionInPath(modelPath string, version uint32) (string, bool, error) {
	var found []string
	versionStr := fmt.Sprintf("%d", version)
	err := filepath.Walk(modelPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if filepath.Base(path) == versionStr &&
				filepath.Dir(path) == modelPath {
				found = append(found, path)
			}
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	switch len(found) {
	case 0:
		return "", false, fmt.Errorf("Failed to find requested version %d in path %s", version, modelPath)
	case 1:
		return found[0], true, nil
	default:
		return "", false, fmt.Errorf("Found multiple folders with version %d %v", version, found)
	}
}

func (t *TritonV2RepositoryHandler) findHighestVersionInPath(modelPath string) (string, bool, error) {
	highestVersionFolderNum := -1
	var highestVersionPath string
	err := filepath.Walk(modelPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path != modelPath {
			dirName := filepath.Base(path)
			i, err := strconv.Atoi(dirName)
			if err != nil {
				return nil
			}
			if i > highestVersionFolderNum {
				highestVersionFolderNum = i
				highestVersionPath = path
			}
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	if highestVersionFolderNum > 0 {
		return highestVersionPath, true, nil
	}
	return modelPath, false, nil
}

func (t *TritonV2RepositoryHandler) loadConfigFromFile(path string) (*pb.ModelConfig, error) {
	dat, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return t.loadConfigFromBytes(dat)
}

func (t *TritonV2RepositoryHandler) loadConfigFromBytes(dat []byte) (*pb.ModelConfig, error) {
	config := pb.ModelConfig{}
	err := prototext.Unmarshal(dat, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (t *TritonV2RepositoryHandler) SetExplainer(modelRepoPath string, explainerSpec *scheduler.ExplainerSpec, envoyHost string, envoyPort int) error {
	return nil
}

func (t *TritonV2RepositoryHandler) SetLlm(modelRepoPath string, llmSpec *scheduler.LlmSpec, envoyHost string, envoyPort int) error {
	return nil
}

func (t *TritonV2RepositoryHandler) SetExtraParameters(modelRepoPath string, parameters []*scheduler.ParameterSpec) error {
	return nil
}

func (t *TritonV2RepositoryHandler) GetModelRuntimeInfo(path string) (*scheduler.ModelRuntimeInfo, error) {
	configPath := filepath.Join(path, TritonConfigFile)
	tritonConfig, err := t.loadConfigFromFile(configPath)
	tritonRuntimeInfo := &scheduler.ModelRuntimeInfo_Triton{
		Triton: &scheduler.TritonModelConfig{
			Cpu: []*scheduler.TritonCPU{
				{InstanceCount: 1},
			},
		},
	}
	if err == nil {
		instanceGroups := tritonConfig.InstanceGroup
		if len(instanceGroups) > 0 {
			var instanceCount int32 = 0
			backend := tritonConfig.Backend
			for _, instanceGroup := range instanceGroups {
				if instanceGroup.Kind == pb.ModelInstanceGroup_KIND_CPU && instanceCount == 0 {
					if instanceGroup.Count < 1 {
						if strings.ToLower(backend) == "tensorflow" || strings.ToLower(backend) == "onnxruntime" {
							instanceCount = 2
						} else {
							instanceCount = 1
						}
					} else {
						instanceCount += instanceGroup.Count
					}
				}
			}
			if instanceCount < 1 {
				instanceCount = 1
			}
			tritonRuntimeInfo.Triton.Cpu = []*scheduler.TritonCPU{{InstanceCount: uint32(instanceCount)}}
		}
	}
	return &scheduler.ModelRuntimeInfo{ModelRuntimeInfo: tritonRuntimeInfo}, nil
}
