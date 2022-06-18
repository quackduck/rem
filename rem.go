package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fatih/color"
	"github.com/otiai10/copy"
)

var (
	version = "dev" // this is set on release build (check .goreleaser.yml)
	helpMsg = `Rem - Get some rem sleep knowing your files are safe
Rem is a CLI Trash
Usage: rem [-t/--set-dir <dir>] [--disable-copy] [--permanent | -u/--undo] <file> ...
       rem [-d/--directory | --empty | -h/--help | -v/--version | -l/--list]
Options:
   -u/--undo              restore a file
   -l/--list              list files in trash
   --empty                empty the trash permanently
   --permanent            delete a file permanently
   -d/--directory         show path to the data dir
   -t/--set-dir <dir>     set the data dir and continue
   -q/--quiet             enable quiet mode
   --disable-copy         if files are on a different fs, don't rename by copy
   -h/--help              print this help message
   -v/--version           print Rem version`
	dataDir               string
	logFileName           = ".trash.log"
	logFile               map[string]string
	renameByCopyIsAllowed = true

	quietMode = false
)

// TODO: Multiple Rem instances could clobber log file. Fix using either file locks or tcp port locks.

func main() {
	if len(os.Args) == 1 {
		handleErrStr("too few arguments")
		fmt.Println(helpMsg)
		return
	}
	if hasOption, _ := argsHaveOption("help", "h"); hasOption {
		fmt.Println(helpMsg)
		return
	}
	if hasOption, _ := argsHaveOption("version", "v"); hasOption {
		fmt.Println("Rem " + version)
		return
	}
	if hasOption, i := argsHaveOptionLong("permanent"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("not enough arguments for --permanent")
			return
		}
		color.Red("Warning, permanently deleting: ")
		printFormattedList(os.Args[i+1:])
		if promptBool("Confirm delete?") {
			for _, filePath := range os.Args[i+1:] {
				permanentlyDeleteFile(filePath)
			}
		}
		return
	}

	dataDir, _ = filepath.Abs(chooseDataDir())
	ignoreArgs := make(map[int]bool, 3)

	if hasOption, i := argsHaveOption("set-dir", "t"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("Not enough arguments for --set-dir")
			return
		}
		dataDir = os.Args[i+1]
		ignoreArgs[i] = true
		ignoreArgs[i+1] = true
	}

	if hasOption, i := argsHaveOption("quiet", "q"); hasOption {
		quietMode = true
		ignoreArgs[i] = true
	}

	if hasOption, _ := argsHaveOption("directory", "d"); hasOption {
		fmt.Println(dataDir)
		return
	}

	logFile = getLogFile()

	if hasOption, _ := argsHaveOption("list", "l"); hasOption {
		printFormattedList(listFilesInTrash())
		return
	}
	if hasOption, _ := argsHaveOptionLong("empty"); hasOption {
		color.Red("Warning, permanently deleting all files in " + dataDir + "/trash")
		if promptBool("Confirm delete?") {
			emptyTrash()
		}
		return
	}
	if hasOption, i := argsHaveOptionLong("disable-copy"); hasOption {
		renameByCopyIsAllowed = false
		ignoreArgs[i] = true
	}
	if hasOption, i := argsHaveOption("undo", "u"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("not enough arguments for --undo")
			return
		}
		for _, filePath := range os.Args[i+1:] {
			restore(filePath)
		}
		return
	}
	// normal case
	ensureTrashDir()
	for i, filePath := range os.Args {
		if i == 0 {
			continue
		}
		if !ignoreArgs[i] {
			trashFile(filePath)
		}
	}
}

func restore(path string) {
	path = filepath.Clean(path)
	absPath, err := filepath.Abs(path)
	if err != nil {
		handleErr(err)
		return
	}

	fileInTrash, ok := logFile[absPath]
	if ok { // found in log
		if renameByCopyIsAllowed {
			err = renameByCopyAllowed(fileInTrash, absPath)
		} else {
			err = os.Rename(fileInTrash, absPath)
		}
		if err != nil {
			handleErr(err)
			return
		}
	} else {
		handleErrStr(color.YellowString(path) + " is not in trash or is missing restore data")
		return
	}
	delete(logFile, absPath)
	setLogFile(logFile) // we deleted an entry so save the edited logFile
	printIfNotQuiet(color.YellowString(path) + " restored")
}

func trashFile(path string) {
	var toMoveTo string
	var err error
	path = filepath.Clean(path)
	toMoveTo = dataDir + "/trash/" + filepath.Base(path)
	if path == toMoveTo { // small edge case when trashing a file from trash
		handleErrStr(color.YellowString(path) + " is already in trash")
		return
	}
	if !exists(path) {
		fi, err := os.Lstat(path)
		if err != nil {
			handleErr(err)
			return
		}
		if !(fi.Mode()&os.ModeSymlink == os.ModeSymlink) {
			handleErrStr(color.YellowString(path) + " does not exist")
			return
		}
		// the file is a broken symlink which will be deleted to match rm behavior
	}
	toMoveTo = getTimestampedPath(toMoveTo, exists)
	path = getTimestampedPath(path, existsInLog)
	if renameByCopyIsAllowed {
		err = renameByCopyAllowed(path, toMoveTo)
	} else {
		err = os.Rename(path, toMoveTo)
	}
	if err != nil {
		handleErr(err)
		return
	}

	absPath, _ := filepath.Abs(path)
	logFile[absPath] = toMoveTo // format is path where it came from ==> path in trash
	setLogFile(logFile)
	// if we've reached here, trashing is complete and successful
	// TODO: Print with quotes only if it contains spaces
	printIfNotQuiet("Trashed " + color.YellowString(path) + "\nUndo using " + color.YellowString("rem --undo \""+path+"\""))
}

func renameByCopyAllowed(src, dst string) error {
	err := os.Rename(src, dst)
	if err != nil {
		err = copy.Copy(src, dst)
		permanentlyDeleteFile(src)
	}
	return err
}

// existsFunc() is used to determine if there is a conflict. It should return true if there is a conflict.
func getTimestampedPath(path string, existsFunc func(string) bool) string {
	var i int // make i accessible in function scope to check if it changed
	oldPath := path
	for ; existsFunc(path); i++ { // big fiasco for avoiding clashes and using smallest timestamp possible along with easter eggs
		switch i {
		case 0:
			path = oldPath + " " + time.Now().Format("Jan 2 15:04:05")
		case 1: // seconds are the same
			path = oldPath + " " + time.Now().Format("Jan 2 15:04:05.000")
		case 2: // milliseconds are same
			path = oldPath + " " + time.Now().Format("Jan 2 15:04:05.000000")
			fmt.Println("No way. This is super unlikely. Please contact my creator at igoel.mail@gmail.com or on github @quackduck and tell him what you were doing.")
		case 3: // microseconds are same
			path = oldPath + " " + time.Now().Format("Jan 2 15:04:05.000000000")
			fmt.Println("You are a god.")
		case 4:
			rand.Seed(time.Now().UTC().UnixNano()) // prep for default case
		default: // nano-freaking-seconds aren't enough for this guy
			fmt.Println("(speechless)")
			if i == 4 { // seed once
				rand.Seed(time.Now().UTC().UnixNano())
			}
			path = oldPath + " " + strconv.FormatInt(rand.Int63(), 10) // add random stuff at the end
		}
	}
	if i != 0 {
		printIfNotQuiet("To avoid conflicts, " + color.YellowString(oldPath) + " will now be called " + color.YellowString(path))
	}
	return path
}

func listFilesInTrash() []string {
	s := make([]string, len(logFile))
	i := 0
	// wd, _ := os.Getwd()
	for key := range logFile {
		// s[i] = strings.TrimPrefix(key, wd+string(filepath.Separator)) // list relative
		s[i] = key
		i++
	}
	return s
}

func emptyTrash() {
	permanentlyDeleteFile(dataDir + "/trash")
	permanentlyDeleteFile(dataDir + "/" + logFileName)
}

func getLogFile() map[string]string {
	if logFile != nil {
		return logFile
	}
	ensureTrashDir()
	b, err := os.ReadFile(dataDir + "/" + logFileName)
	if os.IsNotExist(err) {
		return make(map[string]string)
	}
	if err != nil {
		handleErr(err)
	}
	lines := make(map[string]string)
	err = json.Unmarshal(b, &lines)
	if err != nil {
		handleErr(err)
	}
	return lines
}

func setLogFile(m map[string]string) {
	//f, err := os.OpenFile(trashDir+"/"+logFileName, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644) // truncate to empty, create if not exist, write only
	ensureTrashDir()
	b, err := json.Marshal(m)
	if err != nil {
		handleErr(err)
		return
	}
	err = os.WriteFile(dataDir+"/"+logFileName, b, 0644)
	if err != nil {
		handleErr(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !(os.IsNotExist(err))
}

func existsInLog(elem string) bool {
	_, alreadyExists := logFile[elem]
	return alreadyExists
}

func ensureTrashDir() {
	i, _ := os.Stat(dataDir + "/trash")
	if !exists(dataDir + "/trash") {
		err := os.MkdirAll(dataDir+"/trash", os.ModePerm)
		if err != nil {
			handleErr(err)
			return
		}
		return
	}
	if !i.IsDir() {
		permanentlyDeleteFile(dataDir + "/trash") // not a dir so delete
		ensureTrashDir()                          // then make it
	}
}

// chooseDataDir returns the best directory to store data based on $REM_TRASH and $XDG_DATA_HOME,
// using ~/.local/share/rem as the default if neither is set.
func chooseDataDir() string {
	home, _ := os.UserHomeDir()
	remEnv := os.Getenv("REM_DATADIR")
	if remEnv != "" {
		return remEnv
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome != "" {
		return dataHome + "/rem/trash"
	}
	return home + "/.local/share/rem"
}

func permanentlyDeleteFile(fileName string) {
	err := os.RemoveAll(fileName)
	if err != nil {
		handleErr(err)
	}
}

// Utilities:

func promptBool(promptStr string) (yes bool) {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(promptStr + " (Y/n) > ")
		color.Set(color.FgCyan)
		if !scanner.Scan() {
			break
		}
		color.Unset()
		switch scanner.Text() {
		case "", "y", "Y", "yes", "Yes", "YES", "true", "True", "TRUE":
			return true
		case "n", "N", "no", "No", "NO", "false", "False", "FALSE":
			return false
		default:
			continue
		}
	}
	return true
}

func argsHaveOption(long string, short string) (hasOption bool, foundAt int) {
	for i, arg := range os.Args {
		if arg == "--"+long || arg == "-"+short {
			return true, i
		}
	}
	return false, 0
}

func argsHaveOptionLong(long string) (hasOption bool, foundAt int) {
	for i, arg := range os.Args {
		if arg == "--"+long {
			return true, i
		}
	}
	return false, 0
}

func handleErr(err error) {
	handleErrStr(err.Error())
}

func handleErrStr(str string) {
	_, _ = fmt.Fprintln(os.Stderr, color.RedString("error: ")+str)
}

// keep order
func removeElemFromSlice(slice []string, i int) []string {
	return append(slice[:i], slice[i+1:]...)
}

func printFormattedList(a []string) {
	for i, elem := range a {
		fmt.Println(color.CyanString(strconv.Itoa(i+1)+":"), elem)
	}
}

func printIfNotQuiet(a ...interface{}) {
	if !quietMode {
		fmt.Println(a...)
	}
}
