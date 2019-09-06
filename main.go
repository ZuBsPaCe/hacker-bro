package main

import (
	"flag"
	"fmt"
	"zubspace.com/hacker-bro/app"
	"os"
	"strconv"
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
	activityPtr := rankCommand.String("activity", "100", "Will order stories based on activity and only use the specified percentage of the top stories. Default 100 (=all stories).")
	filterPtr := rankCommand.String("filter", "", "Story and comment filter")

	if len(os.Args) < 2 || (os.Args[1] != "import" && os.Args[1] != "query" && os.Args[1] != "rank" && os.Args[1] != "status" && os.Args[1] != "talk") {
		fmt.Println("Please provide a subcommand: import, query, rank, status")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "import":
		importCommand.Parse(os.Args[2:])
	case "query":
		queryCommand.Parse(os.Args[2:])
	case "rank":
		rankCommand.Parse(os.Args[2:])
	case "status":
		statusCommand.Parse(os.Args[2:])
	case "talk":
		talkCommand.Parse(os.Args[2:])
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

		activity, err := strconv.Atoi(*activityPtr)
		if err != nil || activity <= 0 || activity > 100 {
			rankCommand.PrintDefaults()
			os.Exit(1)
		}

		app.Rank(activity, *filterPtr)

	} else if statusCommand.Parsed() {

		app.Status()

	} else if talkCommand.Parsed() {

		app.Talk()
	}
}
