# Rem

Get some rapid-eye-movement sleep knowing your files are safe.

## What is Rem?

Rem is a CLI trash which makes it _ridiculously_ easy to recover files. We've all had that moment when we've deleted something we realised we shouldn't have. It sucks. Let's fix that!

## Demo

[![asciicast](https://asciinema.org/a/390479.svg)](https://asciinema.org/a/390479?speed=2)

Let's say we have the following file structure
```text
.
├── someDir
│   └── someFile
└── someFile
```

Next, we want to delete `someDir`. Simple!

```shell
rem someDir
```

Now it looks like this:
```text
.
└── someFile
```
Oh no! We actually needed that directory!
```shell
rem --undo someDir
```
Back to:
```text
.
├── someDir
│   └── someFile
└── someFile
```
It's really _that_ easy.

You can also delete files of the same name with no problem:
```shell
rem someDir/someFile someFile
```

## Installing
Build from source or use:
```shell
brew install quackduck/tap/rem
```
## Uninstalling
Simply remove the executable or use:
```shell
brew uninstall rem
```

## Usage

```text
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
   -v/--version           print Rem version
```

Rem stores its data at `$XDG_DATA_HOME/rem` or `.local/share/rem` by default. Alternatively, set the data directory using `$REM_TRASH` or with the `-d` option.

## Thanks

Thanks to [u/skeeto](https://www.reddit.com/user/skeeto/) for helping me with race conditions and design [here](https://www.reddit.com/r/golang/comments/lixr6k/rem_the_trash_cli_that_makes_it_ridiculously_easy/gn7z86z?utm_source=share&utm_medium=web2x&context=3)


