package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

var (
	version = "dev"
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
	home, _      = os.UserHomeDir()
	trashDir     = home + "/.remTrash"
	logFileName  = ".trash.log"
	logSeparator = "\t==>\t"
)

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
		color.Red("Warning, permanently deleting these files in trash: ")
		printFormattedList(listFilesInTrash())
		if promptBool("Confirm delete?") {
			emptyTrash()
		}
		return
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

func listFilesInTrash() []string {
	m := parseLogFile()
	s := make([]string, 0, 10)
	for key := range m {
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
		line := scanner.Text()
		lastLogSeparator := strings.LastIndex(line, logSeparator)
		from := line[:lastLogSeparator]          // up to last logSeparator
		pathInTrash := line[lastLogSeparator+1:] // after last logSeparator
		lines[from] = pathInTrash
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

func restore(path string) {
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
		handleErrStr("file not in trash or missing restore data")
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
	i := 0
	for exists(toMoveTo) { // while it exists (shouldn't) // big fiasco for avoiding clashes and using smallest timestamp possible along with easter eggs
		switch i {
		case 0:
			toMoveTo = trashDir + "/" + filepath.Base(path) + " Deleted at " + time.Now().Format(time.Stamp)
		case 1: // seconds are the same
			toMoveTo = trashDir + "/" + filepath.Base(path) + " Deleted at " + time.Now().Format(time.StampMilli)
			fmt.Println("No way. This is super unlikely. Please contact my creator at igoel.mail@gmail.com or on github @quackduck and tell him what you were doing.")
		case 2: // milliseconds are same
			toMoveTo = trashDir + "/" + filepath.Base(path) + " Deleted at " + time.Now().Format(time.StampMicro)
			fmt.Println("What the actual heck. Please contact him.")
		case 3: // microseconds are same
			toMoveTo = trashDir + "/" + filepath.Base(path) + " Deleted at " + time.Now().Format(time.StampNano)
			fmt.Println("You are a god.")
		case 4:
			rand.Seed(time.Now().UTC().UnixNano()) // prep for default
		default: // nano-freaking-seconds aren't enough for this guy
			fmt.Println("(speechless)")
			if i == 4 { // seed once
				rand.Seed(time.Now().UTC().UnixNano())
			}
			toMoveTo = trashDir + "/" + filepath.Base(path) + strconv.FormatFloat(rand.Float64(), 'E', -1, 64) // add random stuff at the end
		}
		i++
	}
	err = os.Rename(path, toMoveTo)
	if err != nil {
		handleErr(err)
		return
	}
	m := parseLogFile()
	oldPath := path
	i = 1
	for ; existsInMap(m, path); i++ { // might be the same path as before
		path = oldPath + " " + strconv.Itoa(i)
	}
	if i == 1 {
		fmt.Println("A file of this exact path was deleted earlier. To avoid conflicts, this file will now be called " + color.YellowString(path))
	}
	m[path] = toMoveTo // logfile format is path where it came from ==> path in trash
	setLogFile(m)
	fmt.Println("Trashed " + color.YellowString(path) + "\nUndo using " + color.YellowString("rem --undo "+path))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !(os.IsNotExist(err))
}

func existsInMap(m map[string]string, elem string) bool {
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
