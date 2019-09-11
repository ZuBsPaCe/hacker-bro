package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/hacker-bro/app" // WTF: go help importpath
)

func main() {

	// Subcommands / Flags: https://bit.ly/2Lf3igu
	importCommand := flag.NewFlagSet("import", flag.ExitOnError)
	queryCommand := flag.NewFlagSet("query", flag.ExitOnError)
	rankCommand := flag.NewFlagSet("rank", flag.ExitOnError)
	statusCommand := flag.NewFlagSet("status", flag.ExitOnError)
	talkCommand := flag.NewFlagSet("talk", flag.ExitOnError)

	// Import Flags
	dirPtr := importCommand.String("dir", "", "Directory with Json files")

	// Query Flags
	queryPtr := queryCommand.String("q", "", "Database query")

	// Rank Flags
	filterPtr := rankCommand.String("filter", "", "Comment word filter")
	rankConfPtr := rankCommand.String("conf", "", "Output config file path")
	rankCommentLimitPtr := rankCommand.String("commentLimit", "", "Maximum number of comments to look at")

	// Talk Flags
	talkConfPtr := talkCommand.String("conf", "", "Input config file path")

	if len(os.Args) < 2 || (os.Args[1] != "import" && os.Args[1] != "query" && os.Args[1] != "rank" && os.Args[1] != "status" && os.Args[1] != "talk") {
		fmt.Println("Please provide a subcommand: import, query, rank, status")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "import":
		err := importCommand.Parse(os.Args[2:])
		if err != nil {
			fmt.Println("Failed to parse command")
			os.Exit(1)
		}
	case "query":
		err := queryCommand.Parse(os.Args[2:])
		if err != nil {
			fmt.Println("Failed to parse command")
			os.Exit(1)
		}
	case "rank":
		err := rankCommand.Parse(os.Args[2:])
		if err != nil {
			fmt.Println("Failed to parse command")
			os.Exit(1)
		}
	case "status":
		err := statusCommand.Parse(os.Args[2:])
		if err != nil {
			fmt.Println("Failed to parse command")
			os.Exit(1)
		}
	case "talk":
		err := talkCommand.Parse(os.Args[2:])
		if err != nil {
			fmt.Println("Failed to parse command")
			os.Exit(1)
		}
	default:
		flag.PrintDefaults()
		os.Exit(1)
	}

	if importCommand.Parsed() {
		if *dirPtr == "" {
			importCommand.PrintDefaults()
			os.Exit(1)
		}

		app.Import(*dirPtr)

	} else if queryCommand.Parsed() {

		if *queryPtr == "" {
			queryCommand.PrintDefaults()
			os.Exit(1)
		}

		app.Query(*queryPtr)

	} else if rankCommand.Parsed() {

		commentLimit := 0
		var err error

		if *rankCommentLimitPtr != "" {

			commentLimit, err = strconv.Atoi(*rankCommentLimitPtr)
			if err != nil {
				importCommand.PrintDefaults()
				os.Exit(1)
			}
		}

		app.Rank(*filterPtr, *rankConfPtr, commentLimit)

	} else if statusCommand.Parsed() {

		app.Status()

	} else if talkCommand.Parsed() {

		app.Talk(*talkConfPtr)
	}
}
