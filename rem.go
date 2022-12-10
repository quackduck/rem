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
       rem [-d/--directory | --empty | -h/--help | --version | -l/--list]
       rem --rm-mode [oprions] [files]
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
   --version              print Rem version
   --rm-mode              run rem in a mode with extra compatibility with
                          GNU rm; run "rem --rm-mode --help" for more info
   --                     all arguments after this as considered files
                          when deleting files that look like CLI flag, it is
                          advised to place them after --`
    helpRmMode = `rem --rm-mode runs Rem in a mode compatible with GNU rm
This mode changes the output of rem to be quiet by default and adds
additional options and flags.

Usage: rem --rm-mode [oprions] <file> ...
       rem --rm-mode [-d/--directory | --empty | -h/--help | --version | -l/--list]

Oprions:
   -f/--force             Do not print error message on inexistant file,
                          used for compatibility with rm. Also silence prompt
                          of --empry and --permanent
   -v/--verbose           Print a line of logs for each deleted files

Options ignored for compatibility with GNU rm:
   -i/-I/--interactive
   -r/-R/--recursive
   --one-file-system
   --no-preserve-root
   --preserve-root

All the other flags and options available in non-rm mode are also available
with the exeption of the -q/--quiet flag. Run "rem --help" to know their usages.`
	dataDir               string
	logFileName           = ".trash.log"
	logFile               map[string]string

flags = struct {
    renameByCopyIsAllowed  bool
    quietMode              bool
    forceMode              bool
    permanentMode          bool
    rmMode                 bool
    interactiveMode        bool
    verbose                bool
} {
    renameByCopyIsAllowed: true,
    quietMode:             false,
    forceMode:             false,
    permanentMode:         false,
    interactiveMode:       false,
    rmMode:                false,
    verbose:               false,
}
)

// TODO: Multiple Rem instances could clobber log file. Fix using either file locks or tcp port locks.

func main() {
	if len(os.Args) == 1 {
		handleErrStr("too few arguments")
		fmt.Println(helpMsg)
		return
	}

	dataDir, _ = filepath.Abs(chooseDataDir())
	ignoreArgs := make(map[int]bool, 3)
	logFile = getLogFile()

    // Always available arguments
	if hasOption, _ := argsHaveOptionLong("version"); hasOption {
		fmt.Println("Rem " + version)
		return
	}

	if hasOption, i := argsHaveOptionLong("rm-mode"); hasOption {
		flags.rmMode = true
		ignoreArgs[i] = true
    }

	if hasOption, i := argsHaveOptionLong("permanent"); hasOption {
		flags.permanentMode = true
		ignoreArgs[i] = true
    }

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

	if hasOption, i := argsHaveOptionLong("disable-copy"); hasOption {
		flags.renameByCopyIsAllowed = false
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

    // Mode specifics arguments
    if flags.rmMode {
        if hasOption, _ := argsHaveOption("help", "h"); hasOption {
            fmt.Println(helpRmMode)
            return
        }

        if hasOption, i := argsHaveOption("force", "f"); hasOption {
            ignoreArgs[i] = true
            flags.forceMode = true
        }

        if hasOption, i := argsHaveOption("verbose", "v"); hasOption {
            ignoreArgs[i] = true
            flags.verbose = true
        }

        if hasOption, i := argsHaveOption("interactive", "i"); hasOption {
            flags.interactiveMode = true
            ignoreArgs[i] = true
        }
        if hasOption, i := argsHaveOption("interactive", "I"); hasOption {
            flags.interactiveMode = true
            ignoreArgs[i] = true
        }

        // ignored compatibility arguments
        if hasOption, i := argsHaveOption("recursive", "r"); hasOption {
            ignoreArgs[i] = true
        }
        if hasOption, i := argsHaveOption("recursive", "R"); hasOption {
            ignoreArgs[i] = true
        }
        if hasOption, i := argsHaveOptionLong("one-file-system"); hasOption {
            ignoreArgs[i] = true
        }
        if hasOption, i := argsHaveOptionLong("no-preserve-root"); hasOption {
            ignoreArgs[i] = true
        }
        if hasOption, i := argsHaveOptionLong("preserve-root"); hasOption {
            ignoreArgs[i] = true
        }

        // Force flag suppress interactive mode
        if flags.forceMode {
            flags.interactiveMode = false;
        }

    } else {
        if hasOption, _ := argsHaveOption("help", "h"); hasOption {
            fmt.Println(helpMsg)
            return
        }

        if hasOption, i := argsHaveOption("quiet", "q"); hasOption {
            flags.quietMode = true
            ignoreArgs[i] = true
        }
    }

    // Empty left at the end as its behavior depends on mode specifics flags
	if hasOption, _ := argsHaveOptionLong("empty"); hasOption {
		if flags.quietMode || flags.forceMode {
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

	// Making a list of all files to process
    _fileList := make([]string, len(os.Args))
    index := 0
	for i, filePath := range os.Args {
		if i == 0 {
			continue
		}
		if !ignoreArgs[i] {
            _fileList[index] = filePath
            index++
		}
	}
    fileList := _fileList[0:index]

    // Trashing files
    if flags.permanentMode {
        deleteFileList(fileList)
    } else {
        trashFileList(fileList)
    }
}

// Trashes all the files in a list
func trashFileList(fileList []string) {
	ensureTrashDir()
    for _, filePath := range fileList {
        deleteOk := !flags.interactiveMode
        if !deleteOk {
            deleteOk = promptBool("Trashing "+filePath+"?")
        }
        trashFile(filePath)
    }
}

// Permanently deletes all the files in the list
func deleteFileList(fileList []string) {
    deleteOk := flags.forceMode || flags.interactiveMode
    if !deleteOk {
        color.Red("Warning, permanently deleting: ")
        printFormattedList(fileList)
        deleteOk = promptBool("Confirm delete?")
    }
    if deleteOk {
        var err error
        for _, filePath := range fileList {
            deleteOk = !flags.interactiveMode
            if !deleteOk {
                deleteOk = promptBool("Permanently deleting "+filePath+"?")
            }
            err = permanentlyDeleteFile(filePath)
            if err != nil {
                fmt.Println("Could not delete " + filePath)
                handleErr(err)
            }
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
		if flags.renameByCopyIsAllowed {
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
		if !flags.forceMode {
			handleErrStr(color.YellowString(path) + " does not exist")
		}
		return
	}
	toMoveTo = getTimestampedPath(toMoveTo, exists)
	path = getTimestampedPath(path, existsInLog)
	if flags.renameByCopyIsAllowed {
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
    if flags.rmMode {
        if flags.verbose {
            fmt.Println("removed '"+path+"'")
        }
    } else {
        printIfNotQuiet("Trashed " + color.YellowString(path) + "\nUndo using " + color.YellowString("rem --undo \""+path+"\""))
    }
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

func argsHaveOption(long string, short string) (hasOption bool, foundAt int) {
	for i, arg := range os.Args {
		if arg == "--" {
			return false, 0
		}
		if arg == "--"+long || arg == "-"+short {
			return true, i
		}
	}
	return false, 0
}

func argsHaveOptionLong(long string) (hasOption bool, foundAt int) {
	for i, arg := range os.Args {
		if arg == "--" {
			return false, 0
		}
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
	if !flags.quietMode {
		fmt.Println(a...)
	}
}
