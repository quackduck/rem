package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

var (
	version = "dev"
	helpMsg = `Rem - Get some rem sleep knowing your files are safe
Rem is a CLI Trash
Usage: rem [<option>]
       rem [-t/--set-trash <dir>] [-u/--undo | --permanent] file

Options:
   -t/--set-trash <dir>   set trash to dir and continue
   -u/--undo              restore a file
   --permanent            delete a file permanently
<option> can be one of:
   -h/--help              print this help message
   -v/--version           print Rem version
   -d/--directory         show path to trash
   -l/--list              list files in trash`
	home, _      = os.UserHomeDir()
	trashDir     = home + "/.remTrash"
	logFileName  = ".trash.log"
	logSeparator = "   ==>   "
)

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
	if hasOption, _ := argsHaveOption("directory", "d"); hasOption {
		fmt.Println(trashDir)
		return
	}
	if hasOption, _ := argsHaveOption("list", "l"); hasOption {
		printFormattedList(listFilesInTrash())
		return
	}
	if hasOption, i := argsHaveOption("set-trash", "t"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("Not enough arguments for --set-trash")
			return
		}
		//fmt.Println("Using " + os.Args[i+1] + " as trash")
		os.Args = removeElemFromSlice(os.Args, i)
		main()
		return
	}
	if hasOption, _ := argsHaveOptionLong("empty-trash"); hasOption {
		color.Red("Warning, permanently deleting these files in trash: ")
		printFormattedList(listFilesInTrash())
		if promptBool("Confirm delete?") {
			emptyTrash()
		}
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

	if hasOption, i := argsHaveOption("undo", "u"); hasOption {
		if !(len(os.Args) > i+1) {
			handleErrStr("not enough arguments for --undo")
			return
		}
		for _, filePath := range os.Args[i+1:] {
			putBack(filePath)
		}
		return
	}
	// normal case
	ensureTrashDir()
	for _, filePath := range os.Args[1:] {
		trashFile(filePath)
	}
}

func listFilesInTrash() []string {
	m := parseLogFile()
	s := make([]string, 0, 10)
	for key, _ := range m {
		s = append(s, key)
	}
	return s
}

func emptyTrash() {
	permanentlyDeleteFile(trashDir)
}

func parseLogFile() map[string]string {
	ensureTrashDir()
	file, err := os.OpenFile(trashDir+"/"+logFileName, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		handleErr(err)
		return nil
	}
	defer file.Close()
	lines := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines[strings.Split(scanner.Text(), logSeparator)[0]] = strings.Split(scanner.Text(), logSeparator)[1]
	}
	if scanner.Err() != nil {
		handleErr(err)
		return lines
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

	for key, value := range m {
		if _, err = f.WriteString(key + logSeparator + value + "\n"); err != nil {
			handleErr(err)
			return
		}
	}
}

func putBack(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		handleErr(err)
		return
	}
	logFile := parseLogFile()
	fileInTrash, ok := logFile[path]
	if ok {
		err = os.Rename(fileInTrash, path)
		if err != nil {
			handleErr(err)
			return
		}
	} else {
		handleErrStr("file not in trash or missing put back data")
		return
	}
	delete(logFile, path)
	setLogFile(logFile) // we deleted an entry so save the new one
	fmt.Println(color.YellowString(path) + " restored")
}

func trashFile(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		handleErr(err)
		return
	}
	//toMoveTo := trashDir + "/" + filepath.Base(path+time.Now().String())
	toMoveTo := trashDir + "/" + filepath.Base(path)
	if path == toMoveTo { // small edge case when trashing a file from trash
		handleErrStr(color.YellowString(path) + " is already in trash")
		return
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		handleErrStr(color.YellowString(path) + " does not exist")
		return
	}
	if i, err := os.Stat(toMoveTo); !(os.IsNotExist(err)) {
		handleErrStr("file with name " + color.YellowString(i.Name()) + " already in trash at " + color.YellowString(toMoveTo)) // as helpful as possible
		return
	}
	err = os.Rename(path, toMoveTo)
	if err != nil {
		handleErr(err)
		return
	}
	m := parseLogFile()
	m[path] = toMoveTo // logfile format is path where it came from ==> path in trash
	setLogFile(m)
	fmt.Println("Trashed " + color.YellowString(path) + "\nUndo using " + color.YellowString("rem --undo "+path))
}

func ensureTrashDir() {
	if _, err := os.Stat(trashDir); os.IsNotExist(err) {
		err = os.Mkdir(trashDir, os.ModePerm)
		if err != nil {
			handleErr(err)
		}
	}
}

func ensureLogFile() {
	if _, err := os.Stat(trashDir + "/" + logFileName); os.IsNotExist(err) {
		_, err = os.Create(trashDir + "/" + logFileName)
		if err != nil {
			handleErr(err)
		}
	}
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
