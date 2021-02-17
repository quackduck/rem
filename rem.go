package main

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/otiai10/copy"
)

var (
	version = "dev" // this is set on release build (check .goreleaser.yml)
	helpMsg = `Rem - Get some rem sleep knowing your files are safe
Rem is a CLI Trash
Usage: rem [-t/--set-trash <dir>] [--permanent | -u/--undo] file
       rem [-d/--directory | --empty | -h/--help | -v/--version | -l/--list]
Options:
   -u/--undo              restore a file
   -l/--list              list files in trash
   --empty                empty the trash permanently
   --permanent            delete a file permanently
   -d/--directory         show path to trash
   -t/--set-trash <dir>   set trash to dir and continue
   -h/--help              print this help message
   -v/--version           print Rem version`
	home, _               = os.UserHomeDir()
	trashDir              = home + "/.remTrash"
	logFileName           = ".trash.log"
	logFile               map[string]string
	renameByCopyIsAllowed = true
	//logSeparator = "\t==>\t"
)

// TODO: Multiple Rem instances could clobber log file. Fix using either file locks or tcp port locks.
// TODO: Check if files are on different fs and if so, copy it over

func main() {
	trashDir, _ = filepath.Abs(trashDir)
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
	if hasOption, i := argsHaveOption("set-trash", "t"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("Not enough arguments for --set-trash")
			return
		}
		//fmt.Println("Using " + os.Args[i+1] + " as trash")
		trashDir = os.Args[i+1]
		os.Args = removeElemFromSlice(os.Args, i+1) // remove the specified dir too
		os.Args = removeElemFromSlice(os.Args, i)
		main()
		return
	}

	if hasOption, _ := argsHaveOption("directory", "d"); hasOption {
		fmt.Println(trashDir)
		return
	}
	if hasOption, _ := argsHaveOption("list", "l"); hasOption {
		printFormattedList(listFilesInTrash())
		return
	}
	if hasOption, _ := argsHaveOptionLong("empty"); hasOption {
		color.Red("Warning, permanently deleting all files in " + trashDir)
		if promptBool("Confirm delete?") {
			emptyTrash()
		}
		return
	}
	if hasOption, _ := argsHaveOptionLong("disable-copy"); hasOption {
		renameByCopyIsAllowed = false
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
	for _, filePath := range os.Args[1:] {
		trashFile(filePath)
	}
}

func restore(path string) {
	path = filepath.Clean(path)
	absPath, err := filepath.Abs(path)
	if err != nil {
		handleErr(err)
		return
	}
	m := getLogFile()
	fileInTrash, ok := m[absPath]
	if ok {
		err = os.Rename(fileInTrash, absPath)
		if err != nil {
			handleErr(err)
			return
		}
	} else {
		handleErrStr("file not in trash or missing restore data")
		return
	}
	delete(logFile, absPath)
	setLogFile(logFile) // we deleted an entry so save the edited logFile
	fmt.Println(color.YellowString(path) + " restored")
}

func trashFile(path string) {
	var toMoveTo string
	var err error
	path = filepath.Clean(path)
	toMoveTo = trashDir + "/" + filepath.Base(path)
	if path == toMoveTo { // small edge case when trashing a file from trash
		handleErrStr(color.YellowString(path) + " is already in trash")
		return
	}
	if !exists(path) {
		handleErrStr(color.YellowString(path) + " does not exist")
		return
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
	m := getLogFile()
	absPath, _ := filepath.Abs(path)
	m[absPath] = toMoveTo // format is path where it came from ==> path in trash
	setLogFile(m)
	// if we've reached here, trashing is complete and successful
	// TODO: Print with quotes only if it contains spaces
	fmt.Println("Trashed " + color.YellowString(path) + "\nUndo using " + color.YellowString("rem --undo \""+path+"\""))
}

func renameByCopyAllowed(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	lerr := err.(*os.LinkError)
	if lerr.Err == syscall.EXDEV {
		// rename by copying and deleting
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
			path = oldPath + time.Now().Format(time.Stamp)
		case 1: // seconds are the same
			path = oldPath + time.Now().Format(time.StampMilli)
		case 2: // milliseconds are same
			path = oldPath + time.Now().Format(time.StampMicro)
			fmt.Println("No way. This is super unlikely. Please contact my creator at igoel.mail@gmail.com or on github @quackduck and tell him what you were doing.")
		case 3: // microseconds are same
			path = oldPath + time.Now().Format(time.StampNano)
			fmt.Println("You are a god.")
		case 4:
			rand.Seed(time.Now().UTC().UnixNano()) // prep for default case
		default: // nano-freaking-seconds aren't enough for this guy
			fmt.Println("(speechless)")
			if i == 4 { // seed once
				rand.Seed(time.Now().UTC().UnixNano())
			}
			path = oldPath + strconv.FormatInt(rand.Int63(), 10) // add random stuff at the end
		}
	}
	if i != 0 {
		fmt.Println("To avoid conflicts, " + color.YellowString(oldPath) + " will now be called " + color.YellowString(path))
	}
	return path
}

func listFilesInTrash() []string {
	m := getLogFile()
	s := make([]string, len(m))
	i := 0
	// wd, _ := os.Getwd()
	for key := range m {
		// s[i] = strings.TrimPrefix(key, wd+string(filepath.Separator)) // list relative
		s[i] = key
		i++
	}
	return s
}

func emptyTrash() {
	permanentlyDeleteFile(trashDir)
}

func getLogFile() map[string]string {
	if logFile != nil {
		return logFile
	}
	ensureTrashDir()
	file, err := os.OpenFile(trashDir+"/"+logFileName, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		handleErr(err)
		return nil
	}
	defer file.Close()
	lines := make(map[string]string)
	dec := gob.NewDecoder(file)
	err = dec.Decode(&lines)
	if err != nil && err != io.EOF {
		handleErr(err)
	}
	return lines
}

func setLogFile(m map[string]string) {
	//f, err := os.OpenFile(trashDir+"/"+logFileName, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644) // truncate to empty, create if not exist, write only
	ensureTrashDir()
	f, err := os.Create(trashDir + "/" + logFileName)
	if err != nil {
		handleErr(err)
		return
	}
	defer f.Close()
	enc := gob.NewEncoder(f)
	err = enc.Encode(m)
	if err != nil && err != io.EOF {
		handleErr(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !(os.IsNotExist(err))
}

func existsInLog(elem string) bool {
	m := getLogFile()
	_, alreadyExists := m[elem]
	return alreadyExists
}

func ensureTrashDir() {
	i, _ := os.Stat(trashDir)
	if !exists(trashDir) {
		err := os.MkdirAll(trashDir, os.ModePerm)
		if err != nil {
			handleErr(err)
			return
		}
		return
	}
	if !i.IsDir() {
		permanentlyDeleteFile(trashDir) // not a dir so delete
		ensureTrashDir()                // then make it
	}
}

func permanentlyDeleteFile(fileName string) {
	err := os.RemoveAll(fileName)
	if err != nil {
		handleErr(err)
	}
}

//
//func renameByCopyIsAllowed(source, dest string) error {
//	inputFile, err := os.Open(source)
//	if err != nil {
//		return err
//	}
//	defer inputFile.Close()
//	outputFile, err := os.Create(dest)
//	if err != nil {
//		return err
//	}
//	defer outputFile.Close()
//	_, err = io.Copy(outputFile, inputFile)
//	if err != nil {
//		return err
//	}
//	// The copy was successful, so now delete the original file
//	err = os.RemoveAll(source)
//	if err != nil {
//		return err
//	}
//	return nil
//}

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
