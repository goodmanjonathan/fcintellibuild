package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/libgit2/git2go"
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
// use len(ChangedProjectFiles) to determine whether or not project files need to be searched again
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
	repo, err := git.OpenRepository(path)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	statusListStruct, err := repo.StatusList(nil)
	count, err := statusListStruct.EntryCount()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}

	// loop through git status output
	for idx := 0; idx < count; idx++ {
		entry, err := statusListStruct.ByIndex(idx)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		// filter out non source files
		fileName := filepath.Base(entry.IndexToWorkdir.OldFile.Path)
		extension := strings.Split(fileName, ".")[1]
		if extension == CPP {
			source = append(source, filepath.Base(entry.IndexToWorkdir.OldFile.Path))
		} else if extension == CBPROJ {
			cbproj = append(cbproj, filepath.Base(entry.IndexToWorkdir.OldFile.Path))
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

func (conf *runConfig) gatherData(dir string) error {
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
		m[val]++
	}

	for _, val := range two {
		m[val]++
	}

	for _, val := range m {
		if val > 1 {
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
	confFillErr := conf.gatherData(directory)

	changedSourceFiles, changedCbprojFiles := listFilesChanged(directory)

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

		projectsToCompile := make(map[string][]string)
		var wg sync.WaitGroup
		for _, projectFile := range cbprojSlice {
			wg.Add(1)
			go searchCbprojText(projectFile, &wg, changedSourceFiles, &projectsToCompile)
		}

		wg.Wait()

	} else {

	}

	for proj, srcName := range projectsToCompile {
		fmt.Printf("Compiling %v for files: %v\n", proj, srcName)
	}
}
