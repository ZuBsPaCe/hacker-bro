package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	. "strings"
	"time"

	"github.com/bvinc/go-sqlite-lite/sqlite3"
)

const debug = true

type item struct {
	// Shared Json
	Id       int
	ItemType string `json:"type"`
	Deleted  bool

	Kids []int

	// Story Json
	Title string

	// Comment Json
	Text   string
	Parent int

	// Comment custom
	// Level  int
	// Number int

	// Other custom
	fileName string
	items    []item
}

type wordKey struct {
	Pre1 int
	Pre2 int
	Pre3 int
}

type wordInfo struct {
	wordId int
	count  int
}

func Import(dir string) {
	dir = TrimSpace(Trim(dir, "\""))

	stat, err := os.Stat(dir)

	if err != nil {
		fmt.Printf("Path error: %s\n", err)
		os.Exit(1)
	}

	if !stat.IsDir() {
		fmt.Printf("Path is not a directory\n")
		os.Exit(1)
	}

	if dir, err = filepath.Abs(dir); err != nil {
		fmt.Printf("Failed to get absolute path\n")
		os.Exit(1)
	}

	fmt.Printf("Importing data from [%s]\n", dir)

	openDir, err := os.Open(dir)
	if err != nil {
		fmt.Printf("Failed to open directory\n")
	}

	defer openDir.Close()

	dirEntries, err := openDir.Readdir(0)
	if err != nil {
		fmt.Printf("Failed to read directory\n")
		os.Exit(1)
	}

	var fileNames []string

	for i := 0; i < len(dirEntries); i++ {
		if !dirEntries[i].IsDir() {
			fileNames = append(fileNames, dirEntries[i].Name())
		}
	}

	stories := make(map[int]item)
	idToStory := make(map[int]int)

	totalStoryCounter := 0
	validStoryCounter := 0
	deletedStoryCounter := 0
	noCommentsStoryCounter := 0
	emptyStoryCounter := 0

	totalCommentCounter := 0
	validCommentCounter := 0
	deletedCommentCounter := 0
	lostCommentCounter := 0
	emptyCommentCounter := 0

	totalJobCounter := 0
	totalPollCounter := 0

	for i := 0; i < len(fileNames); i++ {
		fmt.Printf("Loading [%s]\n", fileNames[i])

		fileName := fileNames[i]
		filePath := path.Join(dir, fileName)

		openFile, err := os.Open(filePath)
		if err != nil {
			fmt.Printf("Failed to open [%s]\n", fileName)
			os.Exit(1)
		}

		defer openFile.Close()

		var items []item
		lineCounter := 0
		reader := bufio.NewReader(openFile)

		for {
			var lineBuffer bytes.Buffer
			var tempBuffer []byte
			var isPrefix bool

			lineCounter++

			for {
				tempBuffer, isPrefix, err = reader.ReadLine()
				lineBuffer.Write(tempBuffer)

				if !isPrefix {
					break
				}

				if err != nil {
					// EOF
					break
				}
			}

			if err == io.EOF {
				break
			}

			line := lineBuffer.Bytes()

			var item item
			if err := json.Unmarshal(line, &item); err != nil {
				fmt.Printf("Failed to parse [%s] on line [%d]: %s\n", fileName, lineCounter, err)
				os.Exit(1)
			}

			item.fileName = fileName

			items = append(items, item)
		}

		fmt.Printf("Read [%d] items from [%s]\n", len(items), fileNames[i])

		for i := 0; i < len(items); i++ {
			currentItem := items[i]

			if currentItem.ItemType == "story" {
				totalStoryCounter++

				if currentItem.Deleted {
					deletedStoryCounter++
					continue
				}

				if len(currentItem.Title) == 0 {
					emptyStoryCounter++
					continue
				}

				if len(currentItem.Kids) == 0 {
					noCommentsStoryCounter++
					continue
				}

				validStoryCounter++

				stories[currentItem.Id] = currentItem
				idToStory[currentItem.Id] = currentItem.Id

				//fmt.Printf("Line [%d]: Story: %s\n", i+1, currentItem.Title)

			} else if currentItem.ItemType == "comment" {

				totalCommentCounter++

				if currentItem.Deleted {
					deletedCommentCounter++
					continue
				}

				if len(currentItem.Text) == 0 {
					emptyCommentCounter++
					continue
				}

				storyId, parentExists := idToStory[currentItem.Parent]
				if !parentExists {
					//fmt.Printf("Line [%d]: Lost comment [%d]\n", i+1, currentItem.Id)
					lostCommentCounter++
					continue
				}

				validCommentCounter++

				idToStory[currentItem.Id] = storyId

				story := stories[storyId]
				story.items = append(story.items, currentItem)
				stories[storyId] = story

			} else if currentItem.ItemType == "job" {

				totalJobCounter++

			} else if currentItem.ItemType == "poll" || currentItem.ItemType == "pollopt" {

				// TODO: What is the difference between poll and pollopt ?
				totalPollCounter++

			} else {

				fmt.Printf("Line [%d]: Unknown item type [%s]\n", i+1, currentItem.ItemType)
				os.Exit(1)
			}
		}
	}

	fmt.Println()
	fmt.Printf("Total stories: %d\n", totalStoryCounter)
	fmt.Printf("Valid stories: %d\n", validStoryCounter)
	fmt.Printf("Deleted stories: %d\n", deletedStoryCounter)
	fmt.Printf("Empty stories: %d\n", emptyStoryCounter)
	fmt.Printf("Stories with no comments: %d\n", noCommentsStoryCounter)

	fmt.Println()
	fmt.Printf("Total jobs: %d\n", totalJobCounter)

	fmt.Println()
	fmt.Printf("Total polls: %d\n", totalPollCounter)

	fmt.Println()
	fmt.Printf("Total comments: %d\n", totalCommentCounter)
	fmt.Printf("Valid comments: %d\n", validCommentCounter)
	fmt.Printf("Deleted comments: %d\n", deletedCommentCounter)
	fmt.Printf("Empty comments: %d\n", emptyCommentCounter)
	fmt.Printf("Lost comments: %d\n", lostCommentCounter)

	databasePath := "hackerbro.db"

	if fileExists(databasePath) {
		fmt.Printf("Removing old database: %s\n", databasePath)
		os.Remove(databasePath)
	}

	fmt.Println()
	fmt.Printf("Opening database: %s\n", databasePath)

	conn, err := sqlite3.Open(databasePath)
	if err != nil {
		fmt.Printf("Could not open database\n")
		os.Exit(1)
	}

	defer conn.Close()

	err = conn.Exec("CREATE TABLE IF NOT EXISTS Stories(StoryId INTEGER PRIMARY KEY, CommentCount INTEGER, File TEXT)")
	check(err, "Failed to create Stories table")

	err = conn.Exec("CREATE TABLE IF NOT EXISTS Comments(CommentId INTEGER PRIMARY KEY, StoryId INTEGER, Rank INTEGER, File TEXT)")
	check(err, "Failed to create Comments table")

	err = conn.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS StoriesContent USING fts5(Content)")
	check(err, "Failed to create StoriesContent table")

	err = conn.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS CommentsContent USING fts5(Content)")
	check(err, "Failed to create CommentsContent table")

	fmt.Printf("Deleting old data\n")

	deleteItems(conn, "Stories")
	deleteItems(conn, "Comments")

	fmt.Printf("Inserting new data\n")

	progressTime := time.Now()
	progressIteration := 0
	currentStoryIndex := 0
	totalStories := len(stories)

	err = conn.Exec("PRAGMA journal_mode = OFF")
	check(err, "PRAGMA failed")

	err = conn.Exec("PRAGMA synchronous = OFF")
	check(err, "PRAGMA failed")

	err = conn.Begin()
	check(err, "Failed to start transaction")

	for storyId, storyItem := range stories {

		// TODO: Check if len(item.items) == len(item.kids)

		// TODO: Sometimes item.Text seems to be set filled for Stories. When and why?
		err = conn.Exec("INSERT INTO Stories (StoryId, CommentCount, File) Values(?, ?, ?)", storyId, len(storyItem.items), storyItem.fileName)
		check(err, "Failed to insert story")

		err = conn.Exec("INSERT INTO StoriesContent (rowid, Content) Values(?, ?)", storyId, storyItem.Title)
		check(err, "Failed to insert story content")

		for i := 0; i < len(storyItem.items); i++ {
			commentItem := storyItem.items[i]

			err = conn.Exec("INSERT INTO Comments (CommentId, StoryId, Rank, File) Values(?, ?, ?, ?)", commentItem.Id, storyId, 0, commentItem.fileName)
			check(err, "Failed to insert comments")

			//rowId = conn.LastInsertRowID()

			err = conn.Exec("INSERT INTO CommentsContent (rowid, Content) Values(?, ?)", commentItem.Id, commentItem.Text)
			check(err, "Failed to insert story content")
		}

		currentStoryIndex++

		progressIteration++
		if progressIteration > 100 && time.Since(progressTime).Seconds() > 2 {

			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			progress := float64(currentStoryIndex) / float64(totalStories) * 100.0

			fmt.Printf("DB Insert %d of %d / %.1f%% / %0.1f stories per sec\n", currentStoryIndex, totalStories, progress, progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 100
		}
	}

	fmt.Printf("Committing data...\n")

	err = conn.Commit()
	check(err, "Failed to commit transaction")
}

func Query(query string) {

	// Not possible to search for qoutes with fts: https://bit.ly/30O4zSc
	//query = Replace(Trim(query, "\""), "'", "''", -1)
	fmt.Printf("Running Query: [%s]", query)

	conn := openDatabase()
	defer conn.Close()

	storiesFound := 0

	{
		stmt, err := conn.Prepare("SELECT COUNT(*) FROM StoriesContent WHERE Content MATCH ?", query)
		check(err, "Failed to create query statememt")

		defer stmt.Close()

		for {
			hasRows, err := stmt.Step()
			check(err, "Failed to step")

			if !hasRows {
				break
			}

			err = stmt.Scan(&storiesFound)
			check(err, "Failed to scan")
		}
	}

	commentsFound := 0

	{
		stmt, err := conn.Prepare("SELECT COUNT(*) FROM CommentsContent WHERE Content MATCH ?", query)
		check(err, "Failed to create query statememt")

		defer stmt.Close()

		for {
			hasRows, err := stmt.Step()
			check(err, "Failed to step")

			if !hasRows {
				break
			}

			err = stmt.Scan(&commentsFound)
			check(err, "Failed to scan")
		}
	}

	if storiesFound > 0 || commentsFound > 0 {
		fmt.Printf("Stories: %d\n", storiesFound)
		fmt.Printf("Comments: %d\n", commentsFound)
	} else {
		fmt.Println("No results. Sorry.")
	}
}

func Rank(activity int, filter string) {
	fmt.Printf("Ranking comments...")

	conn := openDatabase()
	defer conn.Close()

	// Rank von allen comments auf 0 setzen.
	// Alle Stories suchen die den filter enthalten, entweder in Story-Text oder im Kommentar-Text.
	// Comment-Rank auf 1 setzten, falls die Story enthalten ist.
	// Aktuellen Story und Comment Count loggen.
	// Enthaltene Comments gemäss Story-Activity gegebenenfalls auf 0 setzten.

	storiesCount := queryScalar(conn, "SELECT COUNT(*) FROM Stories")
	//storiesUsedCount := int(float64(storiesCount) / 100.0 * float64(activity))
	commentsCount := queryScalar(conn, "SELECT COUNT(*) FROM Comments")

	fmt.Printf("Selecting comments according to %d%% story activity\n\n", activity)

	var err error

	if activity == 100 {
		err = conn.Exec("UPDATE Comments SET Rank = 1")
		check(err, "Failed to set comments rank")
	} else {
		err = conn.Exec("UPDATE Comments SET Rank = 0")
		check(err, "Failed to set comments rank")

		// err = conn.Exec(
		// 	"WITH cte AS ("+
		// 		"	SELECT StoryId"+
		// 		"	FROM Stories S"+
		// 		"	ORDER BY S.CommentCount DESC"+
		// 		"	LIMIT ?"+
		// 		") "+
		// 		"UPDATE Comments"+
		// 		"	SET Rank = 1"+
		// 		"	WHERE EXISTS (SELECT * FROM cte WHERE cte.StoryId = Comments.StoryId)",
		// 	storiesUsedCount)
		// check(err, "Failed to set comments rank")
	}

	storiesRankedCount := queryScalar(conn, "SELECT COUNT(DISTINCT StoryId) FROM Comments WHERE Comments.Rank > 0")
	storiesRankedRatio := float64(storiesRankedCount) / float64(storiesCount) * 100.0
	fmt.Printf("Using %d stories from %d (%0.1f%%) after applying activity\n", storiesRankedCount, storiesCount, storiesRankedRatio)

	commentsRankedCount := queryScalar(conn, "SELECT COUNT(*) FROM Comments WHERE Rank > 0")
	commentsRankedRatio := float64(commentsRankedCount) / float64(commentsCount) * 100.0
	fmt.Printf("Using %d comments from %d (%0.1f%%) after applying activity\n\n", commentsRankedCount, commentsCount, commentsRankedRatio)

	if len(filter) > 0 {
		fmt.Printf("Selecting comments according to filter [%s]\n\n", filter)

		// err = conn.Exec(
		// 	"WITH cte AS ("+
		// 		"	SELECT rowid AS CommentId FROM CommentsContent WHERE Content MATCH ?"+
		// 		") "+
		// 		"UPDATE Comments"+
		// 		"	SET Rank = 0"+
		// 		"	WHERE NOT EXISTS (SELECT * FROM cte WHERE cte.CommentId = Comments.CommentId)",
		// 	filter,
		// 	filter)

		storiesRankedCount := queryScalar(conn, "SELECT COUNT(DISTINCT StoryId) FROM Comments WHERE Comments.Rank > 0")
		storiesRankedRatio := float64(storiesRankedCount) / float64(storiesCount) * 100.0
		fmt.Printf("Using %d stories from %d (%0.1f%%) after applying filter\n", storiesRankedCount, storiesCount, storiesRankedRatio)

		commentsRankedCount := queryScalar(conn, "SELECT COUNT(*) FROM Comments WHERE Rank > 0")
		commentsRankedRatio := float64(commentsRankedCount) / float64(commentsCount) * 100.0
		fmt.Printf("Using %d comments from %d (%0.1f%%) after applying filter\n", commentsRankedCount, commentsCount, commentsRankedRatio)
	} else {
		fmt.Printf("No filters used\n")
	}
}

func Status() {
	fmt.Printf("Getting status information...")

	conn := openDatabase()
	defer conn.Close()

	storyCount := queryScalar(conn,
		"SELECT COUNT(*) FROM Stories")

	rankedStoryCount := queryScalar(conn,
		//"SELECT COUNT(*) FROM Stories WHERE EXISTS (SELECT * FROM Comments WHERE Comments.StoryId = Stories.StoryId AND Comments.Rank > 0)")
		"SELECT COUNT(DISTINCT StoryId) FROM Comments WHERE Comments.Rank > 0")

	ratio := float64(rankedStoryCount) / float64(storyCount) * 100.0
	fmt.Printf("Using %d stories from %d (%0.1f%%)\n", rankedStoryCount, storyCount, ratio)

	commentCount := queryScalar(conn,
		"SELECT COUNT(*) FROM Comments")

	rankedCommentCount := queryScalar(conn,
		"SELECT COUNT(*) FROM Comments WHERE Rank > 0")

	ratio = float64(rankedCommentCount) / float64(commentCount) * 100.0
	fmt.Printf("Using %d comments from %d (%0.1f%%)\n", rankedCommentCount, commentCount, ratio)
}

func Talk() {
	conn := openDatabase()
	defer conn.Close()

	fmt.Printf("Preparing to query comments...\n")

	stmt, err := conn.Prepare(
		"SELECT Content FROM CommentsContent " +
			"INNER JOIN Comments ON(Comments.CommentId = CommentsContent.rowid) " +
			"WHERE Comments.Rank > 0 " +
			"ORDER BY CommentsContent.rowid ASC")
	check(err, "Failed to select ranked comments")

	defer stmt.Close()

	fmt.Printf("Loading comments...\n")

	progressTime := time.Now()
	progressIteration := 0
	var comments []string

	for {
		hasRows, err := stmt.Step()
		check(err, "Failed to step")

		if !hasRows {
			break
		}

		var comment string
		err = stmt.Scan(&comment)
		check(err, "Failed to scan")

		if debug && len(comment) == 0 {
			fmt.Printf("ERROR: Empty comment detected!\n")
		}

		comments = append(comments, comment)

		progressIteration++
		if progressIteration > 100 && time.Since(progressTime).Seconds() > 2 {
			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			fmt.Printf("Loaded %d comments. %0.1f per sec.\n", len(comments), progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 0
		}
	}

	fmt.Printf("Total comments loaded: %d\n", len(comments))

	reRemoveTags := regexp.MustCompile(`<.*?>`)
	reFindWords := regexp.MustCompile(`[\w'"-]+|\.|,|;|-|:`)

	wordToId := make(map[string]int)
	idToWord := make(map[int]string)
	wordMap := make(map[wordKey][]wordInfo)
	var orderedWordKeys []wordKey

	const wordIdInvalid = 0
	const wordIdDot = 1

	wordToId[""] = wordIdInvalid
	idToWord[wordIdInvalid] = ""

	wordToId["."] = wordIdDot
	idToWord[wordIdDot] = "."

	nextWordId := 2

	addWordMapItem := func(wordId int, key wordKey) {
		if currentInfoSlice, ok := wordMap[key]; !ok {
			currentInfoSlice = make([]wordInfo, 1)
			currentInfoSlice[0] = wordInfo{wordId, 1}
			wordMap[key] = currentInfoSlice
		} else {
			var currentInfo *wordInfo
			for i := 0; i < len(currentInfoSlice); i++ {
				if currentInfoSlice[i].wordId == wordId {
					currentInfo = &currentInfoSlice[i]
					break
				}
			}

			if currentInfo == nil {
				currentInfoSlice = append(currentInfoSlice, wordInfo{wordId, 1})
			} else {
				currentInfo.count++
			}

			wordMap[key] = currentInfoSlice
		}

		orderedWordKeys = append(orderedWordKeys, key)
	}

	progressTime = time.Now()
	totalComments := len(comments)

	for i := 0; i < totalComments; i++ {

		if time.Since(progressTime).Seconds() > 2 {

			progress := float64(i) / float64(totalComments) * 100.0

			fmt.Printf("Analyzed %d of %d (%.01f%%)\n", i, totalComments, progress)

			progressTime = time.Now()
		}

		comment := reRemoveTags.ReplaceAllString(comments[i], "")

		comment = strings.Replace(comment, "&quot;", "", -1)
		comment = strings.Replace(comment, "&#x27;", "'", -1)

		tokens := reFindWords.FindAllString(comment, -1)

		pre1 := wordIdDot
		pre2 := 0
		pre3 := 0

		for j := 0; j < len(tokens); j++ {

			token := tokens[j]

			wordId, ok := wordToId[token]
			if !ok {
				wordId = nextWordId
				wordToId[token] = wordId
				idToWord[wordId] = token
				nextWordId++
			}

			// 1 Word Forward Lookup
			currentKey := wordKey{pre1, 0, 0}
			addWordMapItem(wordId, currentKey)

			if pre2 > 0 {
				// 2 Word Forward Lookup
				currentKey := wordKey{pre1, pre2, 0}
				addWordMapItem(wordId, currentKey)

				if pre3 > 0 {
					// 3 Word Forward Lookup
					currentKey := wordKey{pre1, pre2, pre3}
					addWordMapItem(wordId, currentKey)
				}
			}

			pre3 = pre2
			pre2 = pre1
			pre1 = wordId
		}

		// fmt.Printf(comments[i] + "\n")
		// fmt.Println()
		// fmt.Printf(strings.Join(tokens, " ") + "\n")
		// fmt.Println()
		// fmt.Println()
		// fmt.Println()

		// if i > 3 {
		// 	break;
		// }
	}

	var idSequence []int
	pre1 := 0
	pre2 := 0
	pre3 := 0

	// TODO: Seed randomly in release builds.
	if !debug {
		rand.Seed(time.Now().UTC().UnixNano())
	} else {
		rand.Seed(42)
	}

	{
		fmt.Printf("Find my first word...\n")

		var keysAfterDot []wordKey
		var currentKey wordKey

		// Map iteration order is random! But we want ordered in debug builds! => use orderedWordKeys
		// Order is unimportant in release builds though!

		if !debug {
			for currentKey := range wordMap {
				if currentKey.Pre1 == wordIdDot {
					keysAfterDot = append(keysAfterDot, currentKey)
				}
			}
		} else {
			for _, currentKey := range orderedWordKeys {
				if currentKey.Pre1 == wordIdDot {
					keysAfterDot = append(keysAfterDot, currentKey)
				}
			}
		}

		currentKey = keysAfterDot[rand.Intn(len(keysAfterDot))]
		currentInfoSlice := wordMap[currentKey]

		wordId := currentInfoSlice[rand.Intn(len(currentInfoSlice))].wordId
		idSequence = append(idSequence, wordId)

		pre1 = wordId
	}

	// Re-Seed after initial word was determined.
	if !debug {
		rand.Seed(time.Now().UTC().UnixNano())
	}

	fmt.Printf("Starting to talk...\n")

	nrSentences := 0

	for i := 0; nrSentences < 3 && i < 1000; i++ {

		currentKey := wordKey{pre1, pre2, pre3}
		currentInfoSlice, sequenceFound := wordMap[currentKey]

		if !sequenceFound {
			currentKey := wordKey{pre1, pre2, 0}
			currentInfoSlice, sequenceFound = wordMap[currentKey]

			if !sequenceFound {

				fmt.Printf("No availabe sequence for [%s], [%s], [%s]\n", idToWord[pre3], idToWord[pre2], idToWord[pre1])

				currentKey := wordKey{pre1, 0, 0}
				currentInfoSlice, sequenceFound = wordMap[currentKey]
			}
		}

		var wordId int

		if sequenceFound {
			wordId = currentInfoSlice[rand.Intn(len(currentInfoSlice))].wordId
		} else {
			wordId = wordIdDot
		}

		idSequence = append(idSequence, wordId)

		pre3 = pre2
		pre2 = pre1
		pre1 = wordId

		if wordId == wordIdDot {
			nrSentences++
		}
	}

	fmt.Println()

	punctuations := map[string]struct{}{
		".": struct{}{},
		",": struct{}{},
		"?": struct{}{},
		"!": struct{}{},
		":": struct{}{},
		";": struct{}{}}

	talk := ""
	currentWord := ""

	for _, wordId := range idSequence {
		currentWord = idToWord[wordId]

		if _, ok := punctuations[currentWord]; ok {
			talk += currentWord
		} else {
			talk += " "
			talk += currentWord
		}
	}

	talk = strings.TrimSpace(talk)

	fmt.Printf("Shit HN says:\n\n%s\n", talk)
}

func openDatabase() *sqlite3.Conn {
	databasePath := "hackerbro.db"

	fmt.Println()
	fmt.Printf("Opening database: %s\n", databasePath)

	conn, err := sqlite3.Open(databasePath)
	if err != nil {
		fmt.Printf("Could not open database\n")
		os.Exit(1)
	}

	return conn
}

func queryScalar(conn *sqlite3.Conn, query string) int {
	stmt, err := conn.Prepare(query)
	check(err, "Failed to prepare query")

	defer stmt.Close()

	hasRow, err := stmt.Step()
	check(err, "Failed to step")

	if !hasRow {
		return 0
	}

	var storyCount int
	err = stmt.Scan(&storyCount)
	check(err, "Failed to scan")

	return storyCount
}

func deleteItems(conn *sqlite3.Conn, table string) {
	err := conn.Exec(fmt.Sprintf("DELETE FROM %s", table))
	check(err, fmt.Sprintf("Failed to truncate %s", table))

	err = conn.Exec(fmt.Sprintf("DELETE FROM %sContent", table))
	check(err, fmt.Sprintf("Failed to truncate %sContent", table))
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func check(err error, message string) {
	if err != nil {
		fmt.Printf("Error: %q\n", message)
		fmt.Printf("Details: %q\n", err)
		os.Exit(1)
	}
}
