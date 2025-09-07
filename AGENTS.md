# agent instructions

* short commentary, no fluff, void "You're absolutely right!" and other similar responses
* do not write comments, just the code
* after each task, commit with three sections: (1) a summary of the work (2) an itemised list of actions performed (3) "PROMPT: <user-prompt-verbatim>", and then push

## architecture instructions

* `pkg/client` implements the a ssh client
* `pkg/server` implements the a ssh server
* `cmd/smux` is the `smux` CLI tool
    * it should import `pkg/smux`
* `pkg/smux` implements the ssh-terminal-multiplexer
    * it should import 

## go instructions

* minimise use of package-level variables and functions
    * prefer methods on structs to support encapsulation and testing
    * if you must have package-level variables and functions, then they should aliases singletons and their methods
* check the code compiles with `go build -v -o /dev/null <package>`
* check windows compatibility with `GOOS=windows go build -v -o /dev/null <package>`
* test the code with `go test -v <package>`
* write tests to confirm each step of the plan is working correctly
* prefer early returns
* no `else { return <expr> }`, drop the `else`