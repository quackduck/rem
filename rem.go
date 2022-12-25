package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/otiai10/copy"
)

var (
	version = "dev" // this is set on release build (check .goreleaser.yml)
	helpMsg = `Rem - Get some REM sleep knowing your files are safe
Rem is a CLI Trash

Usage: rem [-t/--set-dir <dir>] [--disable-copy] [--permanent | -u/--undo] <file> ...
       rem [-d/--directory | --empty | -h/--help | --version | -l/--list]
       rem --rm-mode [options] [files]
Options:
   -u/--undo              restore a file
   -l/--list              list files in trash
   --empty                empty the trash permanently
   --permanent            delete a file permanently
   -d/--directory         show path to the data dir
   -t/--set-dir <dir>     set the data dir and continue
   -q/--quiet             enable quiet mode
   --disable-copy         if files are on a different fs, don't move by copying
   -h/--help              print this help message
   --version              print Rem version
   --rm-mode              enable GNU rm compatibility mode
                          run "rem --rm-mode --help" for more info
   --                     all arguments after this are considered files`

	helpRmMode = `rem --rm-mode runs Rem in GNU rm compatibility mode
In this mode, output is quiet by default and additional options are available.

Usage: rem --rm-mode [options] <file> ...

Options:
   -f/--force             Silence error when file does not exist
   -v/--verbose           Print a line for each deleted file
   -i/-I/--interactive    Prompt the user before deleting files

Options ignored for compatibility:
   -r/-R/--recursive
   --one-file-system
   --no-preserve-root
   --preserve-root

All flags and options available in non-rm mode are also available
except for the -q/--quiet flag. Run "rem --help" for usage info.`
	dataDir     string
	logFileName = ".trash.log"
	logFile     map[string]string
	ignoreArgs  = make(map[int]bool)

	flags = struct {
		moveByCopyOk, quiet, force, permanent, rmMode, interactive, verbose bool
	}{moveByCopyOk: true}
)

// TODO: Multiple Rem instances could clobber log file. Fix using either file locks or tcp port locks.

func main() {
	dataDir, _ = filepath.Abs(chooseDataDir())
	logFile = getLogFile()

	// Always available arguments
	if hasOption, _ := argsHaveOption("version", ""); hasOption {
		fmt.Println("Rem " + version)
		return
	}

	updateAndIgnoreIfHasOption("rm-mode", "", &flags.rmMode)
	updateAndIgnoreIfHasOption("permanent", "", &flags.permanent)

	if hasOption, i := argsHaveOption("set-dir", "t"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("Not enough arguments for --set-dir")
			return
		}
		dataDir = os.Args[i+1]
		ignoreArgs[i] = true
		ignoreArgs[i+1] = true
	}

	if hasOption, _ := argsHaveOption("directory", "d"); hasOption {
		fmt.Println(dataDir)
		return
	}

	if hasOption, _ := argsHaveOption("list", "l"); hasOption {
		printFormattedList(listFilesInTrash())
		return
	}

	updateAndIgnoreIfHasOption("disable-copy", "", &flags.moveByCopyOk)

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

	if flags.rmMode {
		flags.quiet = true
		if hasOption, _ := argsHaveOption("help", "h"); hasOption {
			fmt.Println(helpRmMode)
			return
		}

		updateAndIgnoreIfHasOption("force", "f", &flags.force)
		updateAndIgnoreIfHasOption("verbose", "v", &flags.verbose)
		updateAndIgnoreIfHasOption("interactive", "i", &flags.interactive)
		updateAndIgnoreIfHasOption("", "I", &flags.interactive)

		// ignored arguments
		updateAndIgnoreIfHasOption("recursive", "r", nil)
		updateAndIgnoreIfHasOption("", "R", nil)
		updateAndIgnoreIfHasOption("one-file-system", "", nil)
		updateAndIgnoreIfHasOption("preserve-root", "", nil)
		updateAndIgnoreIfHasOption("no-preserve-root", "", nil)
	} else {
		if hasOption, _ := argsHaveOption("help", "h"); hasOption {
			fmt.Println(helpMsg)
			return
		}
		updateAndIgnoreIfHasOption("quiet", "q", &flags.quiet)
	}

	// Empty left at the end as its behavior depends on mode specifics flags
	if hasOption, _ := argsHaveOption("empty", ""); hasOption {
		if flags.quiet || flags.force {
			emptyTrash()
		} else {
			color.Red("Warning, permanently deleting all files in " + dataDir + "/trash")
			if promptBool("Confirm delete?") {
				emptyTrash()
			}
		}
		return
	}

	// Ignoring the first --
	for i, arg := range os.Args {
		if arg == "--" {
			ignoreArgs[i] = true
			break
		}
	}

	// get files to delete
	fileList := getFilesToDelete()

	if len(fileList) == 0 && !flags.force {
		handleErrStr("no files to delete")
		fmt.Println(helpMsg)
		return
	}

	if flags.permanent {
		permanentlyDeleteFiles(fileList)
	} else {
		ensureTrashDir()
		for _, filePath := range fileList {
			if flags.interactive && !promptBool("Trash "+filePath+"?") { // did they say no?
				continue
			}
			trashFile(filePath)
		}
	}
}

func getFilesToDelete() []string {
	files := make([]string, len(os.Args)-1-len(ignoreArgs)) // -1 because of the first elem of os.Args
	index := 0
	for i, file := range os.Args {
		if i == 0 || ignoreArgs[i] {
			continue
		}
		files[index] = file
		index++
	}
	return files
}

func permanentlyDeleteFiles(files []string) {
	if !flags.force && !flags.interactive { // neither force nor interactive (interactive means we ask each time)
		color.Red("Warning, permanently deleting: ")
		printFormattedList(files)
		if !promptBool("Confirm?") {
			return // not ok to delete
		}
	}
	var err error
	for _, file := range files {
		if flags.interactive && !promptBool("Permanently delete "+file+"?") {
			continue // not ok to delete
		}
		err = permanentlyDeleteFile(file)
		if err != nil {
			fmt.Println("Could not delete " + file)
			handleErr(err)
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
		if flags.moveByCopyOk {
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
		if !flags.force {
			handleErrStr(color.YellowString(path) + " does not exist")
		}
		return
	}
	toMoveTo = getTimestampedPath(toMoveTo, exists)
	if flags.moveByCopyOk {
		err = renameByCopyAllowed(path, toMoveTo)
	} else {
		err = os.Rename(path, toMoveTo)
	}
	if err != nil {
		handleErr(err)
		return
	}

	absPath, _ := filepath.Abs(path)

	// make sure there are no conflicts in the log
	timestamped := getTimestampedPath(absPath, existsInLog)

	if timestamped != absPath {
		printIfNotQuiet("To avoid conflicts, " + color.YellowString(filepath.Base(absPath)) + " will now be called " + color.YellowString(filepath.Base(timestamped)))
	}
	logFile[timestamped] = toMoveTo // format is path where it came from ==> path in trash
	setLogFile(logFile)
	// if we've reached here, trashing is complete and successful
	if flags.rmMode {
		if flags.verbose {
			fmt.Println("removed '" + path + "'")
		}
		return
	}
	if strings.ContainsAny(path, " \"'`\t|\\!#$&*(){}[];,<>?^~") {
		path = "'" + path + "'"
	}
	printIfNotQuiet("Trashed " + color.YellowString(path) + "\nUndo using " + color.YellowString("rem --undo "+path))
}

func renameByCopyAllowed(src, dst string) error {
	err := os.Rename(src, dst)
	if err != nil {
		err = copy.Copy(src, dst)
		if err != nil {
			return err
		}
		err = permanentlyDeleteFile(src)
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
	err := permanentlyDeleteFile(dataDir + "/trash")
	if err != nil {
		handleErrStr("Couldn't delete " + dataDir + "/trash " + err.Error())
	}
	err = permanentlyDeleteFile(dataDir + "/" + logFileName)
	if err != nil {
		handleErrStr("Couldn't delete " + dataDir + "/" + logFileName + " " + err.Error())
	}
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
	_, err := os.Lstat(path)
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
		err := permanentlyDeleteFile(dataDir + "/trash") // not a dir so delete
		if err != nil {
			handleErr(err)
			return
		}
		ensureTrashDir() // then make it
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

func permanentlyDeleteFile(fileName string) error {
	err := os.RemoveAll(fileName)
	if err == nil {
		return nil
	}
	err = os.Chmod(fileName, 0700) // make sure we have write permission
	if err != nil {
		return err
	}
	i, err := os.Stat(fileName)
	if err != nil {
		return err
	}
	if i.IsDir() { // recursively chmod
		f, err := os.Open(fileName)
		if err != nil {
			return err
		}
		files, err := f.Readdir(0)
		if err != nil {
			return err
		}
		for _, subFile := range files {
			err = permanentlyDeleteFile(fileName + "/" + subFile.Name())
			if err != nil {
				return err
			}
		}
	}
	err = os.RemoveAll(fileName)
	return err
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

// specify one or both
func argsHaveOption(long string, short string) (hasOption bool, foundAt int) {
	for i, arg := range os.Args {
		if arg == "--" {
			return false, 0
		}
		if long != "" && arg == "--"+long || (short != "" && len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && strings.Contains(arg[1:], short)) {
			return true, i
		}
	}
	return false, 0
}

func updateAndIgnoreIfHasOption(long string, short string, toUpdate *bool) {
	if hasOption, i := argsHaveOption(long, short); hasOption {
		if toUpdate != nil {
			*toUpdate = true
		}
		ignoreArgs[i] = true
	}
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
	if !flags.quiet {
		fmt.Println(a...)
	}
}
