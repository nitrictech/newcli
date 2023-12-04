package env

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/samber/lo"
)

func fileExists(filepath string) bool {
	info, err := os.Stat(filepath)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

var DEFAULT_ENV_FILES = []string{".env", ".env.development"}

// LoadFromDefaultFiles loads environment variables from default files into a map, if a file is not found it is skipped.
func LoadFromDefaultFiles(extra_filepaths ...string) (map[string]string, error) {
	return LoadFromFiles(append(DEFAULT_ENV_FILES, extra_filepaths...)...)
}

// LoadFromFiles loads environment variables from files into a map, if a file is not found it is skipped.
// files are loaded in the order they're provided, if a variable is defined in multiple files the last one wins.
func LoadFromFiles(filepaths ...string) (map[string]string, error) {
	foundEnvFiles := lo.Filter(filepaths, func(filepath string, index int) bool {
		return fileExists(filepath)
	})

	envMap := map[string]string{}

	if len(foundEnvFiles) > 0 {
		var err error
		if envMap, err = godotenv.Read(foundEnvFiles...); err != nil {
			return nil, err
		}
	}
	return envMap, nil
}
