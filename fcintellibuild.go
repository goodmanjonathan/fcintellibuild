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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	cbprojExt          = "cbproj"
	cppExt             = "cpp"
	confFileName       = "fcintellibuild.json"
	nanosecondHour     = 3600000000000
	setFcEnvName       = "SetFcEnv"
	defaultThreadCount = 3
)

// compare ProjectFileMap against `git status` to determine which projects to compile,
type runConfig struct {
	ProjectFileMap   map[string][]string
	lastSetFcEnvTime int64
}

var projectsMapMutex = sync.Mutex{}

func setFcEnv(lastTime int64, dir string) (int64, bool) {
	timeNow := time.Now()
	unixTimeNow := timeNow.UnixNano()
	if unixTimeNow-lastTime > 4*nanosecondHour {
		setfcenv := exec.Command(filepath.Join(dir, setFcEnvName))
		setfcenv.Run()
		return unixTimeNow, true
	} else {
		return lastTime, false
	}
}

func parseArguments() (path string, runSetFcEnv bool, err error) {
	flag.BoolVar(&runSetFcEnv, "setfcenv", false, "force SetFvEnv.cmd to run before compiling")
	flag.Parse()
	if flag.NArg() == 0 {
		return "", runSetFcEnv, errors.New("program requires a path to repo argument")
	} else {
		path, err := filepath.Abs(flag.Args()[0])
		if err != nil {
			return "", runSetFcEnv, err
		}
		return path, runSetFcEnv, nil
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

func (conf *runConfig) unmarshall(dir string) error {
	jsonBuffer, err := ioutil.ReadFile(filepath.Join(dir, CONF_FILE_NAME))
	if err != nil {
		return err
	}
	err = json.Unmarshal(jsonBuffer, conf)
	return err
}

func (conf *runConfig) marshallAndWrite(dir string) error {
	jsonBuffer, err := json.Marshal(conf)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(dir, CONF_FILE_NAME), jsonBuffer, 777)
	if err != nil {
		return err
	}

	return nil
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

func fcmsbuild(projects map[string][]string, pause bool) {
	if pause {

		for proj, srcName := range projects {
			fmt.Printf("Found %v for files: %v\n", proj, srcName)
		}

		fmt.Println("\n--------------")

		reader := bufio.NewReader(os.Stdin)
		for proj, sources := range projects {
			fmt.Printf("Build %v for files %v?  ::  y/n/q  ::  ", proj, sources)
			input, _ := reader.ReadString('\n')
			if input[0] == 'y' {
				cmd := exec.Command("%FCMSBUILD%", proj)
				cmd.Run()
			} else if input[0] == 'q' {
				os.Exit(1)
			} else {
				continue
			}
		}
	} else {
		for proj, sources := range projects {
			fmt.Printf("Building %v for files %v.", proj, sources)
			cmd := exec.Command("%FCMSBUILD%", proj)
			cmd.Run()
		}

		for proj, srcName := range projects {
			fmt.Printf("Compiled %v for files: %v\n", proj, srcName)
		}
	}
}

func main() {
	directory, RunSetFcEnv, err := parseArguments()
	if err != nil {
		fmt.Println(err)
	}

	var conf runConfig
	confFillErr := conf.unmarshall(directory)

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

		conf.ProjectFileMap = projectsToCompile

	} else {
		for projectPath, filesList := range conf.ProjectFileMap {
			if !strIntersectionEmpty(changedSourceFiles, filesList) {
				projectsToCompile[projectPath] = filesList
			}
		}
	}

	if RunSetFcEnv {
		conf.lastSetFcEnvTime, _ = setFcEnv(0, directory)
	} else {
		conf.lastSetFcEnvTime, _ = setFcEnv(conf.lastSetFcEnvTime, directory)
	}

	conf.marshallAndWrite(directory)
	fcmsbuild(projectsToCompile, true)
}
