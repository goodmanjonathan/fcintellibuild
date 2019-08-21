package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"gopkg.in/src-d/go-git.v4"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	CBPROJ         = "cbproj"
	CPP            = "cpp"
	CONF_FILE_NAME = "fcintellibuild.json"
)

// compare ProjectFileMap against `git status` to determine which projects to compile,
type runConfig struct {
	ProjectFileMap map[string][]string
}

var projectsMapMutex = sync.Mutex{}

func parseArguments() (string, error) {
	flag.Parse()
	if flag.NArg() == 0 {
		return "", errors.New("program requires a path to repo argument")
	} else {
		path, err := filepath.Abs(flag.Args()[0])
		if err != nil {
			return "", err
		}
		return path, nil
	}
}

func listFilesChanged(path string) (source, cbproj []string) {
	repo, _ := git.PlainOpen(path)
	worktree, _ := repo.Worktree()
	status, _ := worktree.Status()

	for pathToFile, fileStatus := range status {
		fileExt := strings.Split(pathToFile, ".")[1]
		if (fileStatus.Worktree != git.Unmodified || fileStatus.Staging != git.Unmodified) && fileExt == CPP {
			base := filepath.Base(pathToFile)
			source = append(source, base)
		} else if fileExt == CBPROJ {
			base := filepath.Base(pathToFile)
			cbproj = append(cbproj, base)
		}
	}

	return source, cbproj
}

func searchCbprojText(cbprojPath string, wg *sync.WaitGroup, changedFiles []string, projects *map[string][]string) {

	defer wg.Done()

	f, err := os.Open(cbprojPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	defer f.Close()

	for _, name := range changedFiles {
		_, _ = f.Seek(0, 0)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), name) {
				projectsMapMutex.Lock()
				(*projects)[cbprojPath] = append((*projects)[cbprojPath], name)
				projectsMapMutex.Unlock()
			}
		}
	}
}

func (conf *runConfig) marshall(dir string) error {
	jsonBuffer, err := ioutil.ReadFile(filepath.Join(dir, CONF_FILE_NAME))
	if err != nil {
		return err
	}
	err = json.Unmarshal(jsonBuffer, conf)
	return err
}

func strIntersectionEmpty(one, two []string) bool {
	m := make(map[string]int)
	for _, val := range one {
		m[val] = 1
	}

	for _, val := range two {
		if m[val] == 1 {
			return false
		}
	}

	return true
}

func main() {
	directory, err := parseArguments()
	if err != nil {
		fmt.Println(err)
	}

	var conf runConfig
	confFillErr := conf.marshall(directory)

	changedSourceFiles, changedCbprojFiles := listFilesChanged(directory)
	projectsToCompile := make(map[string][]string)

	if len(changedCbprojFiles) > 0 || confFillErr != nil {
		var cbprojSlice []string

		err = filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			filename := filepath.Base(path)
			splitFilename := strings.Split(filename, ".")
			if info.IsDir() {
				return nil
			}

			if len(splitFilename) != 2 {
				return nil
			}

			if splitFilename[1] != CBPROJ {
				return nil
			}

			cbprojSlice = append(cbprojSlice, path)
			return nil
		})

		var wg sync.WaitGroup
		for _, projectFile := range cbprojSlice {
			wg.Add(1)
			go searchCbprojText(projectFile, &wg, changedSourceFiles, &projectsToCompile)
		}

		wg.Wait()

	} else {
		for projectPath, filesList := range conf.ProjectFileMap {
			if !strIntersectionEmpty(changedSourceFiles, filesList) {
				projectsToCompile[projectPath] = filesList
			}
		}
	}

	for proj, srcName := range projectsToCompile {
		fmt.Printf("Compiling %v for files: %v\n", proj, srcName)
	}
}
