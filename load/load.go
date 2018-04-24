package main

import (
    "flag"
    "log"
    "io/ioutil"
    "os"
    "strings"
    "sync"

    "sart/parse"
    // "sart/module"
)

func worker(wg *sync.WaitGroup, jobs <-chan string) {
    for path := range jobs {
        log.Println(path)

        file, err := os.Open(path)
        if err != nil {
            log.Fatal(err)
        }

        parse.New(path, file)

        file.Close()
    }
    wg.Done()
}

func main() {
    var path string 
    var threads int

    flag.StringVar(&path, "path", "", "path to folder with netlist files")
    flag.IntVar(&threads, "workers", 2, "number of parallel threads to spawn")

    flag.Parse()

    if path == "" {
        flag.PrintDefaults()
        log.Fatal("Insufficient arguments")
    }

    log.SetFlags(log.Lshortfile)

    files, err := ioutil.ReadDir(path)
    if err != nil {
        log.Fatal(err)
    }

    var wg sync.WaitGroup
    jobs := make(chan string, 100)

    for i := 0; i < threads; i++ {
        go worker(&wg, jobs)
        wg.Add(1)
    }

    for _, file := range files {
        filename := file.Name()
        
        if !strings.HasSuffix(filename, ".v") {
            continue
        }

        fpath := path + "/" + filename
        jobs <- fpath
    }

    close(jobs)

    wg.Wait()
}
