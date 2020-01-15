// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"

	"codesearch/index"
)

var usageMessage = `usage: cindex [-list] [-reset] [path...]
Cindex prepares the trigram index for use by csearch.  The index is the
file named by $CSEARCHINDEX, or else $HOME/.csearchindex.
The simplest invocation is
	cindex path...
which adds the file or directory tree named by each path to the index.
For example:
	cindex $HOME/src /usr/include
or, equivalently:
	cindex $HOME/src
	cindex /usr/include
If cindex is invoked with no paths, it reindexes the paths that have
already been added, in case the files have changed.  Thus, 'cindex' by
itself is a useful command to run in a nightly cron job.
The -list flag causes cindex to list the paths it has indexed and exit.
By default cindex adds the named paths to the index but preserves
information about other paths that might already be indexed
(the ones printed by cindex -list).  The -reset flag causes cindex to
delete the existing index before indexing the new paths.
With no path arguments, cindex -reset removes the index.
`

func usage() {
	fmt.Fprintf(os.Stderr, usageMessage)
	os.Exit(2)
}

var (
	listFlag    = flag.Bool("list", false, "list indexed paths and exit")
	resetFlag   = flag.Bool("reset", false, "discard existing index")
	verboseFlag = flag.Bool("verbose", false, "print extra information")
	cpuProfile  = flag.String("cpuprofile", "", "write cpu profile to this file")
	indexFile   = flag.String("index", "", "specific a index file")
	indexDir    = flag.String("indexdir", "", "specific a index dir")
)

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *resetFlag && len(args) == 0 {
		os.Remove(index.File())
		return
	}
	if len(args) == 0 {
		ix := index.Open(index.File())
		for _, arg := range ix.Paths() {
			args = append(args, arg)
		}
		return
	}
	// Translate paths to absolute paths so that we can
	// generate the file list in sorted order.
	for i, arg := range args {
		a, err := filepath.Abs(arg)
		if err != nil {
			log.Printf("%s: %s", arg, err)
			args[i] = ""
			continue
		}
		args[i] = a
	}
	sort.Strings(args)

	for len(args) > 0 && args[0] == "" {
		args = args[1:]
	}

	master := ""

	if *indexDir != "" {
		master = index.IndexFromDir(*indexDir)
	}

	if _, err := os.Stat(master); err != nil { // if indexFileAbs not exist, skipped
		// Does not exist.
		*resetFlag = true
	}

	if *listFlag {
		ix := index.Open(master) // 创建索引文件
		for _, arg := range ix.Paths() {
			fmt.Printf("%s\n", arg)
		}
		return
	}

	file := master // set indexFile here
	if !*resetFlag {
		file += "~"
	}

	ix := index.Create(file) //创建索引
	ix.Verbose = *verboseFlag
	ix.AddPaths(args)
	var indexFileSize int64
	for _, arg := range args {
		log.Printf("index %s", arg)
		filepath.Walk(arg, func(path string, info os.FileInfo, err error) error {
			if _, elem := filepath.Split(path); elem != "" {
				// Skip various temporary or "hidden" files or directories.
				if elem[0] == '.' || elem[0] == '#' || elem[0] == '~' || elem[len(elem)-1] == '~' {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if err != nil {
				log.Printf("%s: %s", path, err)
				return nil
			}
			if info != nil && info.Mode()&os.ModeType == 0 { //os.ModeType
				indexFileSize += info.Size()
				// 这里不能针对索引做判断了，干脆针对被建索引的文件做判断，
				if indexFileSize >= 1*1024*1024*1024*100 { // if bigger than 100G,
					//if indexFileSize >= 1*1024*300 { // 测试大小 > 300KB 一个
					ix.Flush() //归位上一个文件
					indexFileSize = 0
					ix = index.Create(index.IndexFromDir(*indexDir)) //创建索引
				}
				ix.AddFile(path)
			}
			return nil
		})
	}
	log.Printf("flush index")
	ix.Flush() //这里写入索引
	// master 和file是同一个东西，用一个名字即可，但是这个索引需要每次给定咯
	if !*resetFlag {
		log.Printf("merge %s %s", master, file)
		index.Merge(file+"~", master, file)
		os.Remove(file)
		os.Rename(file+"~", master)
	}
	log.Printf("done")
	return
}
