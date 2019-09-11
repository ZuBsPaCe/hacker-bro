package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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

const debug = false

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

type WordKey struct {
	Pre1 int
	Pre2 int
	Pre3 int
}

type wordInfo struct {
	wordId int
	count  int
}

type wordConfig struct {
	Words []string

	WordKeys []WordKey

	// WordKey -> wordId
	WordMap map[int][]int
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

	databasePath := "hacker-bro.db"

	if false && fileExists(databasePath) {
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

	err = conn.Exec("CREATE TABLE IF NOT EXISTS Comments(CommentId INTEGER PRIMARY KEY, StoryId INTEGER, Parent INTEGER, Thread INTEGER, Level INTEGER, Score INTEGER, File TEXT)")
	check(err, "Failed to create Comments table")

	err = conn.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS StoriesContent USING fts5(Content)")
	check(err, "Failed to create StoriesContent table")

	err = conn.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS CommentsContent USING fts5(Content)")
	check(err, "Failed to create CommentsContent table")

	// fmt.Printf("Deleting old data\n")

	// deleteItems(conn, "Stories")
	// deleteItems(conn, "Comments")

	fmt.Printf("Reading known files from database...\n")

	knownFiles := make(map[string]struct{})
	var knownFile string

	{
		stmt, err := conn.Prepare("SELECT DISTINCT File FROM Stories")
		check(err, "Failed to create query statememt")

		defer stmt.Close()

		for {
			hasRows, err := stmt.Step()
			check(err, "Failed to step")

			if !hasRows {
				break
			}

			err = stmt.Scan(&knownFile)
			check(err, "Failed to scan")

			knownFiles[knownFile] = struct{}{}
		}
	}

	readStoryCounter := 0
	newStoryCounter := 0
	deletedStoryCounter := 0
	noCommentsStoryCounter := 0
	askHnStoryCounter := 0
	emptyStoryCounter := 0

	readCommentCounter := 0
	newCommentCounter := 0
	deletedCommentCounter := 0
	emptyCommentCounter := 0

	readJobCounter := 0
	readPollCounter := 0

	var newStories []item
	var newComments []item

	newStoriesMap := make(map[int]item)

	for i := 0; i < len(fileNames); i++ {

		fileName := fileNames[i]
		filePath := path.Join(dir, fileName)

		fmt.Printf("Loading [%s]...\n", fileName)

		if _, hasKey := knownFiles[fileName]; hasKey {
			fmt.Printf("Skipping [%s]. Already imported.\n", fileName)
			continue
		}

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
				readStoryCounter++

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

				if strings.HasPrefix(currentItem.Title, "Ask HN:") {
					askHnStoryCounter++
					continue
				}

				newStoryCounter++
				newStories = append(newStories, currentItem)
				newStoriesMap[currentItem.Id] = currentItem

			} else if currentItem.ItemType == "comment" {

				readCommentCounter++

				if currentItem.Deleted {
					deletedCommentCounter++
					continue
				}

				if len(currentItem.Text) == 0 {
					emptyCommentCounter++
					continue
				}

				newCommentCounter++
				newComments = append(newComments, currentItem)

			} else if currentItem.ItemType == "job" {

				readJobCounter++

			} else if currentItem.ItemType == "poll" || currentItem.ItemType == "pollopt" {

				// TODO: What is the difference between poll and pollopt ?
				readPollCounter++

			} else {

				fmt.Printf("Line [%d]: Unknown item type [%s]\n", i+1, currentItem.ItemType)
				os.Exit(1)
			}
		}
	}

	fmt.Println()
	fmt.Printf("Read stories: %d\n", readStoryCounter)
	fmt.Printf("New valid stories: %d\n", newStoryCounter)
	fmt.Printf("Deleted stories: %d\n", deletedStoryCounter)
	fmt.Printf("Empty stories: %d\n", emptyStoryCounter)
	fmt.Printf("Ask HN stories: %d\n", askHnStoryCounter)
	fmt.Printf("Stories with no comments: %d\n", noCommentsStoryCounter)

	fmt.Println()
	fmt.Printf("Read jobs: %d\n", readJobCounter)

	fmt.Println()
	fmt.Printf("Read polls: %d\n", readPollCounter)

	fmt.Println()
	fmt.Printf("Read comments: %d\n", readCommentCounter)
	fmt.Printf("New valid comments: %d\n", newCommentCounter)
	fmt.Printf("Deleted comments: %d\n", deletedCommentCounter)
	fmt.Printf("Empty comments: %d\n", emptyCommentCounter)

	fmt.Printf("Inserting new data\n")

	progressTime := time.Now()
	progressIteration := 0
	currentStoryIndex := 0
	newStoriesCount := len(newStories)
	newCommentsCount := len(newComments)

	// err = conn.Exec("PRAGMA journal_mode = OFF")
	// check(err, "PRAGMA failed")

	// err = conn.Exec("PRAGMA synchronous = OFF")
	// check(err, "PRAGMA failed")

	err = conn.Begin()
	check(err, "Failed to start transaction")

	for _, storyItem := range newStories {

		// TODO: Check if len(item.items) == len(item.kids)

		// TODO: Sometimes item.Text seems to be set filled for Stories. When and why?
		err = conn.Exec("INSERT INTO Stories (StoryId, File, CommentCount) Values(?, ?, 0)", storyItem.Id, storyItem.fileName)
		check(err, "Failed to insert story")

		err = conn.Exec("INSERT INTO StoriesContent (rowid, Content) Values(?, ?)", storyItem.Id, storyItem.Title)
		check(err, "Failed to insert story content")

		currentStoryIndex++

		progressIteration++
		if progressIteration > 100 && time.Since(progressTime).Seconds() > 2 {

			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			progress := float64(currentStoryIndex) / float64(newStoriesCount) * 100.0

			fmt.Printf("DB Insert %d of %d / %.1f%% / %0.1f stories per sec\n", currentStoryIndex, newStoriesCount, progress, progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 100
		}
	}

	progressTime = time.Now()
	progressIteration = 0
	currentCommentIndex := 0

	for _, commentItem := range newComments {

		err = conn.Exec("INSERT INTO Comments (CommentId, StoryId, Parent, Thread, Level, Score, File) Values(?, 0, ?, 0, 0, 0, ?)", commentItem.Id, commentItem.Parent, commentItem.fileName)
		check(err, "Failed to insert comments")

		err = conn.Exec("INSERT INTO CommentsContent (rowid, Content) Values(?, ?)", commentItem.Id, commentItem.Text)
		check(err, "Failed to insert story content")

		currentCommentIndex++

		progressIteration++
		if progressIteration > 100 && time.Since(progressTime).Seconds() > 2 {

			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			progress := float64(currentCommentIndex) / float64(newCommentsCount) * 100.0

			fmt.Printf("DB Insert %d of %d / %.1f%% / %0.1f comments per sec\n", currentCommentIndex, newCommentsCount, progress, progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 100
		}
	}

	fmt.Printf("Committing data...\n")

	err = conn.Commit()
	check(err, "Failed to commit transaction")

	fmt.Printf("Loading story id's and comment count...\n")

	storyId := 0
	commentCount := 0
	storyCommentCount := make(map[int]int)
	storyIds := make(map[int]struct{})

	{
		stmt, err := conn.Prepare("SELECT StoryId, CommentCount FROM Stories")
		check(err, "Failed to create query statememt")

		defer stmt.Close()

		for {
			hasRows, err := stmt.Step()
			check(err, "Failed to step")

			if !hasRows {
				break
			}

			err = stmt.Scan(&storyId, &commentCount)
			check(err, "Failed to scan")

			storyCommentCount[storyId] = commentCount
			storyIds[storyId] = struct{}{}
		}
	}

	fmt.Printf("Loading comment tree...\n")

	storyId = 0
	commentId := 0
	commentParent := 0

	commentParents := make(map[int]int)
	var commentsToCheck []int

	{
		stmt, err := conn.Prepare("SELECT StoryId, CommentId, Parent FROM Comments")
		check(err, "Failed to create query statememt")

		defer stmt.Close()

		for {
			hasRows, err := stmt.Step()
			check(err, "Failed to step")

			if !hasRows {
				break
			}

			err = stmt.Scan(&storyId, &commentId, &commentParent)
			check(err, "Failed to scan")

			commentParents[commentId] = commentParent

			if storyId == 0 {
				commentsToCheck = append(commentsToCheck, commentId)
			}
		}
	}

	fmt.Printf("Setting comment parents...\n")

	lostCommentCount := 0
	storyWithNewCommentCount := make(map[int]struct{})

	err = conn.Begin()
	check(err, "Failed to start transaction")

	for _, commentId := range commentsToCheck {

		storyId, level, threadId := findParent(commentParents, storyIds, commentId, 1)
		if storyId > 0 {

			thread := 0
			story, hasKey := newStoriesMap[storyId]
			if hasKey {
				for threadIdIndex, currentThreadId := range story.Kids {
					if currentThreadId == threadId {
						thread = threadIdIndex + 1
						break
					}
				}
			}

			err := conn.Exec("UPDATE Comments SET StoryId = ?, Thread = ?, Level = ? WHERE CommentId = ?", storyId, thread, level, commentId)
			check(err, "Failed to set comment parent")

			storyCommentCount[storyId] += 1
			storyWithNewCommentCount[storyId] = struct{}{}
		} else {
			lostCommentCount++
		}
	}

	fmt.Printf("Setting new comment count of stories...\n")

	for storyId := range storyWithNewCommentCount {
		commentCount := storyCommentCount[storyId]
		err := conn.Exec("UPDATE Stories SET CommentCount = ? WHERE StoryId = ?", commentCount, storyId)
		check(err, "Failed to set comment parent")
	}

	err = conn.Commit()
	check(err, "Failed to commit transaction")
}

func findParent(commentParents map[int]int, storyIds map[int]struct{}, commentId int, level int) (foundStoryId int, foundLevel int, threadId int) {
	if parentId, hasKey := commentParents[commentId]; hasKey {
		if _, hasKey := storyIds[parentId]; hasKey {
			return parentId, level, commentId
		}
		return findParent(commentParents, storyIds, parentId, level+1)
	} else {
		return 0, 0, 0
	}
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

func Rank(filter string, outPath string, commentLimit int) {
	fmt.Printf("Ranking comments...")

	conn := openDatabase()
	defer conn.Close()

	fmt.Printf("Initializing comment score...\n")

	var err error

	err = conn.Exec("UPDATE Comments SET Score = 0")
	check(err, "Failed to set comments score")

	if filter != "" {
		fmt.Printf("Creating temporary table for filtered comments...\n")

		err = conn.Exec("CREATE TABLE IF NOT EXISTS temp.FilteredComments (CommentId INT PRIMARY KEY)")
		check(err, "Failed to create temp table")

		err = conn.Exec("DELETE FROM temp.FilteredComments")
		check(err, "Failed to truncate temp table")

		err = conn.Exec(
			"INSERT INTO temp.FilteredComments "+
				"SELECT rowid "+
				"FROM CommentsContent WHERE Content MATCH ?",
			filter)
		check(err, "Failed to filter comments")
	}

	fmt.Printf("Creating temporary table for filtered stories...\n")

	err = conn.Exec("CREATE TABLE IF NOT EXISTS temp.FilteredStories (StoryId INT PRIMARY KEY)")
	check(err, "Failed to create temp table")

	err = conn.Exec("DELETE FROM temp.FilteredStories")
	check(err, "Failed to truncate temp table")

	minCommentCount := 20

	fmt.Printf("Searching stories with at least %d comments\n", minCommentCount)

	err = conn.Exec(
		"INSERT INTO temp.FilteredStories "+
			"SELECT rowid "+
			"FROM Stories WHERE CommentCount >= ?",
		minCommentCount)
	check(err, "Failed to filter comments")

	fmt.Printf("Setting comment scores 1\n")

	err = conn.Exec("UPDATE Comments SET Score = 1 WHERE Thread <= 3 ")
	check(err, "Failed to increase comments score")

	fmt.Printf("Setting comment scores 2\n")

	err = conn.Exec("UPDATE Comments SET Score = Score + 1 WHERE Thread <= 3 AND Level <= 2")
	check(err, "Failed to increase comments score")

	fmt.Printf("Setting comment scores 3\n")

	err = conn.Exec("UPDATE Comments SET Score = Score + 1 WHERE EXISTS (SELECT 1 FROM temp.FilteredStories WHERE Comments.StoryId = temp.FilteredStories.StoryId)")
	check(err, "Failed to increase comments score")

	if filter != "" {
		fmt.Printf("Setting comment scores 4\n")

		err = conn.Exec("UPDATE Comments SET Score = 0 WHERE NOT EXISTS (SELECT 1 FROM temp.FilteredComments WHERE Comments.CommentId = temp.FilteredComments.CommentId)")
		check(err, "Failed to increase comments score")
	}

	fmt.Printf("Setting comment scores 5\n")

	err = conn.Exec("UPDATE Comments SET Score = 0 WHERE StoryId = 0")
	check(err, "Failed to set comments score")

	fmt.Printf("Setting comment scores done\n")

	/*
		commentScores := make(map[int]int)

		{
			var stmt *sqlite3.Stmt

			if filter != "" {
				fmt.Printf("Loading comment ids with filter [%s]...\n", filter)

				stmt, err = conn.Prepare("SELECT CommentId FROM Comments INNER JOIN CommentsContent ON (CommentsContent.rowid = Comments.CommentId) WHERE Comments.StoryId > 0 AND CommentsContent.Content MATCH ?", filter)
				check(err, "Failed to create query statememt")
			} else {
				fmt.Printf("Loading comment ids without filter...\n")

				stmt, err = conn.Prepare("SELECT CommentId FROM Comments WHERE StoryId > 0")
				check(err, "Failed to create query statememt")
			}

			defer stmt.Close()

			var commentId int

			for {
				// hasRows, err := stmt.Step()
				// check(err, "Failed to step")

				// if !hasRows {
				// 	break
				// }

				// var commentId int

				// err = stmt.Scan(&commentId)
				// check(err, "Failed to scan")

				// commentScores[commentId] = 0

				hasRows, _ := stmt.Step()

				if !hasRows {
					break
				}

				//commentId, _, _ = stmt.ColumnInt(0)
				stmt.Scan(&commentId)
				commentScores[commentId] = 0
			}
		}

		{
			var stmt *sqlite3.Stmt

			fmt.Printf("Increase score for comments with low thread number...\n")

			stmt, err = conn.Prepare("SELECT CommentId FROM Comments WHERE Thread <= 3")
			check(err, "Failed to create query statememt")

			defer stmt.Close()

			for {
				hasRows, err := stmt.Step()
				check(err, "Failed to step")

				if !hasRows {
					break
				}

				var commentId int

				err = stmt.Scan(&commentId)
				check(err, "Failed to scan")

				if _, hasKey := commentScores[commentId]; hasKey {
					commentScores[commentId] = 1
				}
			}
		}

		{
			var stmt *sqlite3.Stmt

			fmt.Printf("Increase score for comments with low thread number and low level...\n")

			stmt, err = conn.Prepare("SELECT CommentId FROM Comments WHERE Thread <= 3 AND Level <= 2")
			check(err, "Failed to create query statememt")

			defer stmt.Close()

			for {
				hasRows, err := stmt.Step()
				check(err, "Failed to step")

				if !hasRows {
					break
				}

				var commentId int

				err = stmt.Scan(&commentId)
				check(err, "Failed to scan")

				if _, hasKey := commentScores[commentId]; hasKey {
					commentScores[commentId] += 1
				}
			}
		}

		{
			var stmt *sqlite3.Stmt

			fmt.Printf("Increase score for comments in threads with high participation...\n")

			stmt, err = conn.Prepare("SELECT CommentId FROM Comments INNER JOIN Stories ON (Comments.StoryId = Stories.StoryId) WHERE Stories.CommentCount >= 20")
			check(err, "Failed to create query statememt")

			defer stmt.Close()

			for {
				hasRows, err := stmt.Step()
				check(err, "Failed to step")

				if !hasRows {
					break
				}

				var commentId int

				err = stmt.Scan(&commentId)
				check(err, "Failed to scan")

				if _, hasKey := commentScores[commentId]; hasKey {
					commentScores[commentId] += 1
				}
			}
		}

		{
			fmt.Printf("Writing new scores...")

			conn.Begin()

			for commentId, score := range commentScores {

				if score == 0 {
					continue
				}

				err = conn.Exec("UPDATE Comments SET Score = ? WHERE CommentId = ?", score, commentId)
				check(err, "Failed to update score")
			}

			conn.Commit()
		}
	*/

	totalStoriesCount := queryScalar(conn, "SELECT COUNT(*) FROM Stories")
	fmt.Printf("Total stories: %d\n", totalStoriesCount)

	usedStoriesCount := queryScalar(conn, "SELECT COUNT(DISTINCT StoryId) FROM Comments WHERE Score > 0")
	fmt.Printf("Used stories: %d\n", usedStoriesCount)

	totalCommentsCount := queryScalar(conn, "SELECT COUNT(*) FROM Comments")
	fmt.Printf("Total comments: %d\n", totalCommentsCount)

	usedComments := queryScalar(conn, "SELECT COUNT(*) FROM Comments WHERE Score > 0")
	fmt.Printf("Used comments: %d\n", usedComments)

	if len(outPath) > 0 {
		fmt.Printf("Preparing output file\n")

		wordConf := generateWordMap(conn, commentLimit)

		//jsonString, err := json.Marshal(wordConf)
		jsonString, err := json.MarshalIndent(wordConf, "", "\t")
		check(err, "Failed to serialize config file\n")

		err = ioutil.WriteFile(outPath, jsonString, os.ModePerm)
		check(err, "Failed to write config file\n")
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

func Talk(wordConfigPath string) {

	var wordConf wordConfig

	if len(wordConfigPath) == 0 {
		conn := openDatabase()
		defer conn.Close()

		wordConf = generateWordMap(conn, 500)
	} else {
		file, err := ioutil.ReadFile(wordConfigPath)
		check(err, "Failed to read config file\n")

		err = json.Unmarshal(file, &wordConf)
		check(err, "Failed to unserialize config file\n")
	}

	wordMap := make(map[WordKey][]int)

	for currentWordKeyIndex, currentWordKey := range wordConf.WordKeys {
		wordMap[currentWordKey] = wordConf.WordMap[currentWordKeyIndex]
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

	const wordIdDot = 1

	{
		fmt.Printf("Find my first word...\n")

		var keysAfterDot []WordKey
		var currentKey WordKey

		// Map iteration order is random! But we want ordered in debug builds! => use orderedWordKeys
		// Order is unimportant in release builds though!

		// if !debug {
		for currentKey := range wordMap {
			if currentKey.Pre1 == wordIdDot {
				keysAfterDot = append(keysAfterDot, currentKey)
			}
		}
		// } else {
		// 	for _, currentKey := range orderedWordKeys {
		// 		if currentKey.Pre1 == wordIdDot {
		// 			keysAfterDot = append(keysAfterDot, currentKey)
		// 		}
		// 	}
		// }

		currentKey = keysAfterDot[rand.Intn(len(keysAfterDot))]
		currentWordIds := wordMap[currentKey]

		wordId := currentWordIds[rand.Intn(len(currentWordIds))]
		idSequence = append(idSequence, wordId)

		pre1 = wordId
	}

	// Re-Seed after initial word was determined.
	if !debug {
		rand.Seed(time.Now().UTC().UnixNano())
	}

	fmt.Printf("Starting to talk...\n")

	nrSentences := 0
	determinism := 0

	for i := 0; nrSentences < 3 && i < 1000; i++ {

		currentKey := WordKey{pre1, pre2, pre3}
		currentWordIds, sequenceFound := wordMap[currentKey]

		// TODO: needsShuffle Logic is unoptimized...

		if determinism > 3 || !sequenceFound {
			currentKey := WordKey{pre1, pre2, 0}
			currentWordIds, sequenceFound = wordMap[currentKey]

			if determinism > 5 || !sequenceFound {

				currentKey := WordKey{pre1, 0, 0}
				currentWordIds, sequenceFound = wordMap[currentKey]
			}
		}

		var wordId int

		if sequenceFound {
			if len(currentWordIds) == 1 {
				wordId = currentWordIds[0]

				determinism += 1
			} else {
				wordId = currentWordIds[rand.Intn(len(currentWordIds))]

				determinism = 0
			}
		} else {
			fmt.Printf("No availabe sequence for [%s], [%s], [%s]\n", wordConf.Words[pre3], wordConf.Words[pre2], wordConf.Words[pre1])
			wordId = wordIdDot

			determinism = 0
		}

		//if determinism > 3 {
		fmt.Printf("Determinism: %d\n", determinism)
		//}

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
		currentWord = wordConf.Words[wordId]

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

func generateWordMap(conn *sqlite3.Conn, commentLimit int) wordConfig {

	fmt.Printf("Preparing to query comments...\n")

	commentLimitPostfix := ""
	if commentLimit > 0 {
		commentLimitPostfix = "LIMIT " + string(commentLimit)
	}

	stmt, err := conn.Prepare(
		"SELECT Content FROM CommentsContent " +
			"INNER JOIN Comments ON(Comments.CommentId = CommentsContent.rowid) " +
			"WHERE SCORE > 0 " +
			"ORDER BY Comments.Score DESC " +
			commentLimitPostfix)
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
	reRemoveUrls := regexp.MustCompile(`\bhttps?\:.*?(\s|$)`)
	reFindWords := regexp.MustCompile(`[\w'"-]+|\.|,|;|-|:`)

	wordToId := make(map[string]int)
	idToWord := make(map[int]string)
	wordMap := make(map[WordKey][]wordInfo)
	wordList := make([]string, 0)

	//var orderedWordKeys []WordKey

	const wordIdInvalid = 0
	const wordIdDot = 1

	wordToId[""] = wordIdInvalid
	idToWord[wordIdInvalid] = ""
	wordList = append(wordList, "")

	wordToId["."] = wordIdDot
	idToWord[wordIdDot] = "."
	wordList = append(wordList, ".")

	nextWordId := 2

	addWordMapItem := func(wordId int, key WordKey) {
		if currentWordIds, ok := wordMap[key]; !ok {
			currentWordIds = make([]wordInfo, 1)
			currentWordIds[0] = wordInfo{wordId, 1}
			wordMap[key] = currentWordIds
		} else {
			var currentInfo *wordInfo
			for i := 0; i < len(currentWordIds); i++ {
				if currentWordIds[i].wordId == wordId {
					currentInfo = &currentWordIds[i]
					break
				}
			}

			if currentInfo == nil {
				currentWordIds = append(currentWordIds, wordInfo{wordId, 1})
			} else {
				currentInfo.count++
			}

			wordMap[key] = currentWordIds
		}

		//orderedWordKeys = append(orderedWordKeys, key)
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
		comment = reRemoveUrls.ReplaceAllString(comment, " ")

		comment = strings.Replace(comment, "&quot;", "", -1)
		comment = strings.Replace(comment, "&#x27;", "'", -1)
		comment = strings.Replace(comment, "&gt;", ">", -1)
		comment = strings.Replace(comment, "&lt;", "<", -1)

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
				wordList = append(wordList, token)
				nextWordId++
			}

			// 1 Word Forward Lookup
			currentKey := WordKey{pre1, 0, 0}
			addWordMapItem(wordId, currentKey)

			if pre2 > 0 {
				// 2 Word Forward Lookup
				currentKey := WordKey{pre1, pre2, 0}
				addWordMapItem(wordId, currentKey)

				if pre3 > 0 {
					// 3 Word Forward Lookup
					currentKey := WordKey{pre1, pre2, pre3}
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

	var outwordConfig wordConfig

	outwordConfig.Words = wordList
	outwordConfig.WordKeys = make([]WordKey, 0)
	outwordConfig.WordMap = make(map[int][]int)

	wordKeyIndex := 0
	for currentWordKey, currentWordInfos := range wordMap {

		if len(currentWordInfos) <= 3 {
			for _, currentWordInfo := range currentWordInfos {
				outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
			}
		} else {
			topCount1 := 0
			topCount2 := 0
			topCount3 := 0

			for _, currentWordInfo := range currentWordInfos {
				if currentWordInfo.count > topCount1 {
					topCount3 = topCount2
					topCount2 = topCount1
					topCount1 = currentWordInfo.count
				} else if currentWordInfo.count > topCount2 {
					topCount3 = topCount2
					topCount2 = currentWordInfo.count
				} else if currentWordInfo.count > topCount3 {
					topCount3 = currentWordInfo.count
				}
			}

			for _, currentWordInfo := range currentWordInfos {
				if currentWordInfo.count == topCount3 || currentWordInfo.count == topCount2 || currentWordInfo.count == topCount1 {
					outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
				}
			}
		}

		outwordConfig.WordKeys = append(outwordConfig.WordKeys, currentWordKey)
		wordKeyIndex += 1

		// if len(currentWordInfos) <= 8 {
		// 	for _, currentWordInfo := range currentWordInfos {
		// 		outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
		// 	}
		// } else {
		// 	median := 0
		// 	for _, currentWordInfo := range currentWordInfos {
		// 		median += currentWordInfo.count
		// 	}

		// 	median = int(math.Ceil(float64(median) / float64(len(currentWordInfos))))

		// 	for _, currentWordInfo := range currentWordInfos {
		// 		if currentWordInfo.count >= median {
		// 			outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
		// 		}
		// 	}
		// }

		// wordKeyIndex += 1
	}

	return outwordConfig
}

func openDatabase() *sqlite3.Conn {
	databasePath := "hacker-bro.db"

	fmt.Println()
	fmt.Printf("Opening database: %s\n", databasePath)

	conn, err := sqlite3.Open(databasePath)
	if err != nil {
		fmt.Printf("Could not open database\n")
		os.Exit(1)
	}

	// err = conn.Exec(
	// 	"PRAGMA page_size = 4096;" +
	// 		"PRAGMA cache_size=10000;" +
	// 		"PRAGMA locking_mode=EXCLUSIVE;" +
	// 		"PRAGMA synchronous=NORMAL;" +
	// 		"PRAGMA journal_mode=WAL;" +
	// 		"PRAGMA cache_size=5000;")

	err = conn.Exec(
		"PRAGMA page_size = 4096;" +
			"PRAGMA cache_size=10000;" +
			"PRAGMA locking_mode=EXCLUSIVE;" +
			"PRAGMA synchronous=OFF;" +
			"PRAGMA journal_mode=OFF;" +
			"PRAGMA cache_size=5000;")
	check(err, "PRAGMA failed")

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

// func deleteItems(conn *sqlite3.Conn, table string) {
// 	err := conn.Exec(fmt.Sprintf("DELETE FROM %s", table))
// 	check(err, fmt.Sprintf("Failed to truncate %s", table))

// 	err = conn.Exec(fmt.Sprintf("DELETE FROM %sContent", table))
// 	check(err, fmt.Sprintf("Failed to truncate %sContent", table))
// }

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
