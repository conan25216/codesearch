package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	// "x-patrol/util/codesearch/index"

	"codesearch/index"

	"codesearch/regexp"
)

var usageMessage = `usage: csearch [-c] [-f fileregexp] [-h] [-i] [-l] [-n] regexp
Csearch behaves like grep over all indexed files, searching for regexp,
an RE2 (nearly PCRE) regular expression.
The -c, -h, -i, -l, and -n flags are as in grep, although note that as per Go's
flag parsing convention, they cannot be combined: the option pair -i -n
cannot be abbreviated to -in.
The -f flag restricts the search to files whose names match the RE2 regular
expression fileregexp.
Csearch relies on the existence of an up-to-date index created ahead of time.
To build or rebuild the index that csearch uses, run:
	cindex path...
where path... is a list of directories or individual files to be included in the index.
If no index exists, this command creates one.  If an index already exists, cindex
overwrites it.  Run cindex -help for more.
Csearch uses the index stored in $CSEARCHINDEX or, if that variable is unset or
empty, $HOME/.csearchindex.
`

func usage() {
	fmt.Fprintf(os.Stderr, usageMessage)
	os.Exit(2)
}

var (
	fFlag       = flag.String("f", "", "search only files with names matching this regexp")
	iFlag       = flag.Bool("i", false, "case-insensitive search")
	verboseFlag = flag.Bool("verbose", false, "print extra information")
	concur      = flag.Bool("concur", false, "choose concurrent mode")
	bruteFlag   = flag.Bool("brute", false, "brute force - search all files in index")
	cpuProfile  = flag.String("cpuprofile", "", "write cpu profile to this file")
	indexFile   = flag.String("indexfile", "", "read index from specific file")
	indexDir    = flag.String("indexdir", "", "specific a index dir")

	matches bool
)

func Main() {
	g := regexp.Grep{ //创建一个自定义的正则对象, 这个正则对象核心居然是两个输出，奇怪
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		N:      true,
	}
	g.AddFlags()
	g.N = true

	flag.Usage = usage
	flag.Parse()
	args := flag.Args()

	if len(args) != 1 {
		usage()
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	fmt.Println(args[0])
	pat := "(?m)" + args[0]
	if *iFlag {
		pat = "(?i)" + pat
	}
	// fmt.Println("args[0]:", args[0])
	// fmt.Println("pat:", pat)

	re, err := regexp.Compile(pat)
	if err != nil {
		log.Fatal(err)
	}
	g.Regexp = re
	var fre *regexp.Regexp
	if *fFlag != "" {
		fre, err = regexp.Compile(*fFlag)
		if err != nil {
			log.Fatal(err)
		}
	}

	q := index.RegexpQuery(re.Syntax) // RegexpQuery returns a Query for the given regexp.
	// fmt.Printf("+%v", q)
	if *verboseFlag {
		log.Printf("query: %s\n", q)
	}

	var indexdir string

	if *indexDir != "" {
		indexdir = *indexDir
	}

	var post []uint32
	if *concur {
		var wg sync.WaitGroup

		fileLst, err := ioutil.ReadDir(indexdir)
		fmt.Println("fileLst length for wg number: ", len(fileLst))
		wg.Add(len(fileLst))
		if err != nil {
			return
		}

		for _, file := range fileLst {
			go func(info os.FileInfo) {
				path := filepath.Clean(indexdir + "/" + info.Name())
				defer wg.Done()
				if info.IsDir() {
					return
				}
				if err != nil {
					log.Printf("%s: %s", path, err)
					return
				}
				if info.Size() > 0 {
					path, _ := filepath.Abs(path)
					// fmt.Printf("go search on %+v", path)
					ix := index.Open(path)
					// log.Printf("open path\n, %s", path)
					ix.Verbose = *verboseFlag
					// var post []uint32
					if *bruteFlag {
						post = ix.PostingQuery(&index.Query{Op: index.QAll})
					} else {
						// 递归搜索，这里应该没问题，问题没出在这。
						post = ix.PostingQuery(q)
					}
					if *verboseFlag {
						log.Printf("post query identified %d possible files\n", len(post))
					}

					// 明天看这里干了啥
					if fre != nil {
						// log.Printf("in the query, fre = \n, %+v", fre)
						fnames := make([]uint32, 0, len(post))

						for _, fileid := range post {
							name := ix.Name(fileid)
							if fre.MatchString(name, true, true) < 0 {
								continue
							}
							fnames = append(fnames, fileid)
						}

						if *verboseFlag {
							log.Printf("filename regexp matched %d files\n", len(fnames))
						}
						post = fnames
					}

					// log.Printf("post is = \n, %+v", post)

					for _, fileid := range post {
						name := ix.Name(fileid)
						g.File(name) // 208 error
						// log.Printf("g File = \n, %+v", name)
					}
					matches = g.Match
				}
			}(file)
			// go func(info os.FileInfo) {
			// 	fmt.Println(filepath.Clean(indexdir + "/" + info.Name()))
			// 	wg.Done()
			// }(file)
		}
		wg.Wait()
	}
	filepath.Walk(indexdir, func(path string, info os.FileInfo, err error) error {
		// fmt.Printf("go search on %+v", path)
		if info.IsDir() {
			return nil
		}
		if err != nil {
			log.Printf("%s: %s", path, err)
			return nil
		}
		if info.Size() > 0 {
			// fmt.Printf("go search on %+v", path)
			ix := index.Open(path)
			ix.Verbose = *verboseFlag
			// var post []uint32
			if *bruteFlag {
				post = ix.PostingQuery(&index.Query{Op: index.QAll})
			} else {
				post = ix.PostingQuery(q)
			}
			if *verboseFlag {
				log.Printf("post query identified %d possible files\n", len(post))
			}

			if fre != nil {
				fnames := make([]uint32, 0, len(post))

				for _, fileid := range post {
					name := ix.Name(fileid)
					if fre.MatchString(name, true, true) < 0 {
						continue
					}
					fmt.Printf("fnames is %v", fnames)
					fnames = append(fnames, fileid)
				}
				fmt.Printf("fnames is %v", fnames)

				if *verboseFlag {
					log.Printf("filename regexp matched %d files\n", len(fnames))
				}
				post = fnames
			}

			for _, fileid := range post {
				name := ix.Name(fileid)
				g.File(name)
			}

			matches = g.Match
		}
		return nil
	})
	// runtime.GOMAXPROCS(10)

}

func main() {
	Main()
	if !matches {
		os.Exit(1)
	}
	os.Exit(0)
}
