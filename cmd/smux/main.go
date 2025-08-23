package main

import (
	"log"

	"github.com/jpillora/opts"
)

type config struct {
	Debug bool `opts:""`
}

func main() {
	c := config{}
	opts.New(&c).Name("smux").Parse()
	log.Printf("%v", c)
}
