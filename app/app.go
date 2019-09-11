package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	. "strings"
	"time"

	"github.com/bvinc/go-sqlite-lite/sqlite3"
)

var reFindWords *regexp.Regexp

func init() {
	reFindWords = regexp.MustCompile(`[\w'"-]+|\.|,|;|-|:`)
}

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
}

type WordKey struct {
	Pre1 int
	Pre2 int
	Pre3 int
}

type wordInfo struct {
	wordId int
	score  int
}

type wordConfig struct {
	Words []string

	WordKeys []WordKey

	// WordKey -> wordId / score
	WordMap    map[int][]int
	WordScores map[int][]int
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

	err = conn.Exec("CREATE TABLE IF NOT EXISTS Comments(CommentId INTEGER PRIMARY KEY, StoryId INTEGER, Parent INTEGER, Thread INTEGER, Level INTEGER, File TEXT)")
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

	stmtInsertStories, err := conn.Prepare("INSERT INTO Stories (StoryId, File, CommentCount) Values(?, ?, 0)")
	check(err, "Failed to prepare statement")
	defer stmtInsertStories.Close()

	stmtInsertStoriesContent, err := conn.Prepare("INSERT INTO StoriesContent (rowid, Content) Values(?, ?)")
	check(err, "Failed to prepare statement")
	defer stmtInsertStoriesContent.Close()

	for _, storyItem := range newStories {

		// TODO: Check if len(item.items) == len(item.kids)

		// TODO: Sometimes item.Text seems to be set filled for Stories. When and why?
		_ = stmtInsertStories.Exec(storyItem.Id, storyItem.fileName)
		_ = stmtInsertStoriesContent.Exec(storyItem.Id, storyItem.Title)

		currentStoryIndex++

		progressIteration++
		if progressIteration%1000 == 0 && time.Since(progressTime).Seconds() > 2 {

			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			progress := float64(currentStoryIndex) / float64(newStoriesCount) * 100.0

			fmt.Printf("DB Insert %d of %d / %.1f%% / %0.1f stories per sec\n", currentStoryIndex, newStoriesCount, progress, progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 0
		}
	}

	progressTime = time.Now()
	progressIteration = 0
	currentCommentIndex := 0

	reRemoveTags := regexp.MustCompile(`<.*?>`)
	reRemoveUrls := regexp.MustCompile(`\bhttps?\:.*?(\s|$)`)
	reRemoveQuoteStarts := regexp.MustCompile(`(?m)^>+\s*`)
	reRemoveSingleQuotes := regexp.MustCompile(`[^\w]'|'[\w]`) // Want to remove 'this', but not I'm.
	reRemoveBraces := regexp.MustCompile(`\[.*?\]`)

	stmtInsertComments, err := conn.Prepare("INSERT INTO Comments (CommentId, StoryId, Parent, Thread, Level, File) Values(?, 0, ?, 0, 0, ?)")
	check(err, "Failed to prepare statement")
	defer stmtInsertComments.Close()

	stmtInsertCommentsContent, err := conn.Prepare("INSERT INTO CommentsContent (rowid, Content) Values(?, ?)")
	check(err, "Failed to prepare statement")
	defer stmtInsertCommentsContent.Close()

	for _, commentItem := range newComments {

		comment := commentItem.Text

		if strings.Contains(comment, "&") {
			// comment = strings.Replace(comment, "&quot;", "", -1)
			comment = strings.Replace(comment, "&quot;", "", -1)  // Double Quotes: "
			comment = strings.Replace(comment, "&#x27;", "'", -1) // Single Quotes: '
			comment = strings.Replace(comment, "&#x2F;", "/", -1)
			comment = strings.Replace(comment, "&gt;", ">", -1)
			comment = strings.Replace(comment, "&lt;", "<", -1)
			comment = strings.Replace(comment, "&amp;", "&", -1)
		}

		comment = reRemoveTags.ReplaceAllString(comment, " ")
		comment = reRemoveBraces.ReplaceAllString(comment, " ")
		comment = reRemoveUrls.ReplaceAllString(comment, " ")
		comment = reRemoveQuoteStarts.ReplaceAllString(comment, "")
		comment = reRemoveSingleQuotes.ReplaceAllString(comment, "")

		_ = stmtInsertComments.Exec(commentItem.Id, commentItem.Parent, commentItem.fileName)
		_ = stmtInsertCommentsContent.Exec(commentItem.Id, comment)

		currentCommentIndex++

		progressIteration++
		if progressIteration%1000 == 0 && time.Since(progressTime).Seconds() > 2 {

			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			progress := float64(currentCommentIndex) / float64(newCommentsCount) * 100.0

			fmt.Printf("DB Insert %d of %d / %.1f%% / %0.1f comments per sec\n", currentCommentIndex, newCommentsCount, progress, progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 0
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

	stmtUpdateComments, err := conn.Prepare("UPDATE Comments SET StoryId = ?, Thread = ?, Level = ? WHERE CommentId = ?")
	check(err, "Failed to prepare statement")

	defer stmtUpdateComments.Close()

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

			_ = stmtUpdateComments.Exec(storyId, thread, level, commentId)

			storyCommentCount[storyId] += 1
			storyWithNewCommentCount[storyId] = struct{}{}
		} else {
			lostCommentCount++
		}
	}

	fmt.Printf("Setting new comment count of stories...\n")

	stmtUpdateCommentCounts, err := conn.Prepare("UPDATE Stories SET CommentCount = ? WHERE StoryId = ?")
	check(err, "Failed to prepare statement")

	defer stmtUpdateCommentCounts.Close()

	for storyId := range storyWithNewCommentCount {
		commentCount := storyCommentCount[storyId]

		_ = stmtUpdateCommentCounts.Exec(commentCount, storyId)
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

func Rank(filter string, outPath string, commentLimit int, verbose bool) {
	fmt.Printf("Ranking comments...")

	conn := openDatabase()
	defer conn.Close()

	fmt.Printf("Initializing comment score...\n")

	var err error

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
			hasRows, _ := stmt.Step()

			if !hasRows {
				break
			}

			_ = stmt.Scan(&commentId)
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
			hasRows, _ := stmt.Step()

			if !hasRows {
				break
			}

			var commentId int

			_ = stmt.Scan(&commentId)

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
			hasRows, _ := stmt.Step()

			if !hasRows {
				break
			}

			var commentId int

			_ = stmt.Scan(&commentId)

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
			hasRows, _ := stmt.Step()

			if !hasRows {
				break
			}

			var commentId int

			_ = stmt.Scan(&commentId)

			if _, hasKey := commentScores[commentId]; hasKey {
				commentScores[commentId] += 1
			}
		}
	}

	{
		fmt.Printf("Storing new scores in a temporary table...\n")

		err = conn.Exec("CREATE TABLE IF NOT EXISTS temp.Scores (CommentId INT PRIMARY KEY, Score INT)")
		check(err, "Failed to create temp table")

		err = conn.Exec("DELETE FROM temp.Scores")
		check(err, "Failed to truncate temp table")

		err = conn.Begin()
		check(err, "Failed to start transaction")

		for commentId, score := range commentScores {

			if score == 0 {
				continue
			}

			_ = conn.Exec("INSERT INTO temp.Scores (CommentId, Score) VALUES (?, ?)", commentId, score)
		}

		err = conn.Commit()
		check(err, "Failed to start transaction")
	}

	totalStoriesCount := queryScalar(conn, "SELECT COUNT(*) FROM Stories")
	fmt.Printf("Total stories: %d\n", totalStoriesCount)

	usedStoriesCount := queryScalar(conn, "SELECT COUNT(DISTINCT StoryId) FROM Comments INNER JOIN temp.Scores ON (Comments.CommentId = temp.Scores.CommentId)")
	fmt.Printf("Used stories: %d\n", usedStoriesCount)

	totalCommentsCount := queryScalar(conn, "SELECT COUNT(*) FROM Comments")
	fmt.Printf("Total comments: %d\n", totalCommentsCount)

	usedComments := queryScalar(conn, "SELECT COUNT(*) FROM temp.Scores")
	fmt.Printf("Used comments: %d\n", usedComments)

	fmt.Printf("Preparing output file\n")

	wordConf := generateWordMap(conn, commentLimit, verbose)

	var jsonString []byte

	if verbose {
		fmt.Printf("Serializing output file (indented)\n")

		jsonString, err = json.MarshalIndent(wordConf, "", "\t")
		check(err, "Failed to serialize config file\n")
	} else {
		jsonString, err = json.Marshal(wordConf)
		check(err, "Failed to serialize config file\n")
	}

	fmt.Printf("Writing output file\n")

	err = ioutil.WriteFile(outPath, jsonString, os.ModePerm)
	check(err, "Failed to write config file\n")

	fmt.Printf("Done: [%s]\n", outPath)
}

func Status() {
	fmt.Printf("Getting status information...")

	conn := openDatabase()
	defer conn.Close()

	fileCount := queryScalar(conn,
		"SELECT COUNT(DISTINCT File) FROM Stories")

	fmt.Printf("%d files\n", fileCount)

	storyCount := queryScalar(conn,
		"SELECT COUNT(*) FROM Stories")

	fmt.Printf("%d stories\n", storyCount)

	commentCount := queryScalar(conn,
		"SELECT COUNT(*) FROM Comments")

	fmt.Printf("%d comments\n", commentCount)
}

func Talk(wordConfigPath string, talkCount int, continuity int, stability int, talkInit string, randSeed1 int, randSeed2 int, verbose bool) {

	var wordConf wordConfig

	fmt.Printf("Reading word map [%s]...\n", wordConfigPath)

	file, err := ioutil.ReadFile(wordConfigPath)
	check(err, "Failed to read config file\n")

	fmt.Printf("Parsing word map...\n")

	err = json.Unmarshal(file, &wordConf)
	check(err, "Failed to unserialize config file\n")

	fmt.Printf("Preparing word map...\n")

	wordMap := make(map[WordKey][]wordInfo)

	for currentWordKeyIndex, currentWordKey := range wordConf.WordKeys {
		nextWordCount := len(wordConf.WordMap[currentWordKeyIndex])

		wordMap[currentWordKey] = make([]wordInfo, nextWordCount)

		for i := 0; i < nextWordCount; i++ {
			wordMap[currentWordKey][i] = wordInfo{wordConf.WordMap[currentWordKeyIndex][i], wordConf.WordScores[currentWordKeyIndex][i]}
		}
	}

	var randInit *rand.Rand
	var randTalk *rand.Rand

	for i := 0; i < talkCount; i++ {

		if i == 0 {
			if randSeed1 == 0 {
				randSeed1 = int(time.Now().UnixNano() % math.MaxInt32)
			}
		} else {
			randSeed1 = randInit.Int()
		}

		randInit = rand.New(rand.NewSource(int64(randSeed1)))

		if verbose {
			fmt.Printf("Using randInit seed [%d]\n", randSeed1)
		}

		if i == 0 {
			if randSeed2 == 0 {
				randSeed2 = int(time.Now().UnixNano() % math.MaxInt32)
			}
		} else {
			randSeed2 = randTalk.Int()
		}

		randTalk = rand.New(rand.NewSource(int64(randSeed2)))

		if verbose {
			fmt.Printf("Using randTalk seed [%d]\n", randSeed2)
		}

		createTalk(wordConf.Words, wordConf.WordKeys, wordMap, continuity, stability, talkInit, randInit, randTalk, verbose)
	}
}

func createTalk(words []string, wordKeys []WordKey, wordMap map[WordKey][]wordInfo, continuity int, stability int, talkInit string, randInit *rand.Rand, randTalk *rand.Rand, verbose bool) {

	const wordIdDot = 1

	var idSequence []int
	pre1 := 0
	pre2 := 0
	pre3 := 0

	if talkInit == "" {
		{
			if verbose {
				fmt.Println()
				fmt.Printf("Find my first word...\n")
			}

			var keysAfterDot []WordKey

			// Map iteration order is random! But we want ordered in verbose builds! => use wordKeys

			var wordKey WordKey
			for _, wordKey = range wordKeys {
				if wordKey.Pre1 == wordIdDot {
					keysAfterDot = append(keysAfterDot, wordKey)
				}
			}

			wordKey = keysAfterDot[randInit.Intn(len(keysAfterDot))]
			currentWordInfos := wordMap[wordKey]

			wordId := currentWordInfos[randInit.Intn(len(currentWordInfos))].wordId
			idSequence = append(idSequence, wordId)

			pre1 = wordId
		}

		if verbose {
			fmt.Printf("Using first word [%s]\n", words[pre1])
		}
	} else {
		talkInit = strings.TrimSpace(strings.Trim(talkInit, "\""))
		tokens := reFindWords.FindAllString(talkInit, -1)

		for _, token := range tokens {
			pre3 = pre2
			pre2 = pre1
			pre1 = 0

			for index, word := range words {
				if word == token {
					pre1 = index
					break
				}
			}
		}

		if verbose {
			fmt.Println()
			fmt.Printf("Using start of sentence [%s]\n", talkInit)
		}
	}

	nrSentences := 0
	chainCount := 0

	for i := 0; nrSentences < 3 && i < 1000; i++ {

		currentKey := WordKey{pre1, pre2, pre3}
		currentWordInfos, sequenceFound := wordMap[currentKey]

		// TODO: needsShuffle Logic is unoptimized...

		if verbose && chainCount > continuity {
			fmt.Printf("Continuity detected: Chain: %d. Allowed: %d\n", chainCount, continuity)
		}

		if !sequenceFound || chainCount > continuity {
			currentKey = WordKey{pre1, pre2, 0}
			currentWordInfos, sequenceFound = wordMap[currentKey]

			if sequenceFound && len(currentWordInfos) > 1 {
				chainCount = 0
			}

			if !sequenceFound || chainCount > continuity {

				currentKey = WordKey{pre1, 0, 0}
				currentWordInfos, sequenceFound = wordMap[currentKey]
			}
		}

		var wordId int

		if sequenceFound {

			var wordInfo wordInfo

			wordInfoCount := len(currentWordInfos)

			if stability > 1 && wordInfoCount > 1 {
				wordInfoCount = int(math.Ceil((1.0 - float64(stability)/100.0) * float64(wordInfoCount)))
				if wordInfoCount < 1 {
					wordInfoCount = 1
				}
			}

			if wordInfoCount == 1 {
				wordInfo = currentWordInfos[0]
				chainCount += 1
			} else {

				scoreSum := 0
				for i := 0; i < wordInfoCount; i++ {
					scoreSum += currentWordInfos[i].score
				}

				// Hint: Items in currentWordInfos are sorted by Descending Score (High scores first)

				randScore := randTalk.Intn(scoreSum)

				scoreSum = 0
				for i := 0; i < wordInfoCount; i++ {
					currentWordInfo := currentWordInfos[i]

					scoreSum += currentWordInfo.score
					wordInfo = currentWordInfo

					if randScore < scoreSum {
						break
					}
				}

				chainCount = 0
			}

			wordId = wordInfo.wordId

			if verbose {
				fmt.Printf("[%s], [%s], [%s] =>", words[currentKey.Pre3], words[currentKey.Pre2], words[currentKey.Pre1])

				for i := 0; i < wordInfoCount; i++ {
					currentWordInfo := currentWordInfos[i]
					fmt.Printf(" %d=[%s]", currentWordInfo.score, words[currentWordInfo.wordId])
				}

				fmt.Printf(" => Using [%s]\n", words[wordId])
			}
		} else {
			if verbose {
				fmt.Printf("No availabe sequence for [%s], [%s], [%s]\n", words[pre3], words[pre2], words[pre1])
			}

			wordId = wordIdDot

			chainCount = 0
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
		currentWord = words[wordId]

		if _, ok := punctuations[currentWord]; ok {
			talk += currentWord
		} else {
			talk += " "
			talk += currentWord
		}
	}

	talk = strings.TrimSpace(talk)

	if talkInit != "" {

		if len(talk) > 0 {
			if _, ok := punctuations[string(talk[0])]; !ok {
				talkInit += " "
			}
		}

		talk = talkInit + talk
	}

	fmt.Printf("Shit HN says:\n\n%s\n", talk)
}

func generateWordMap(conn *sqlite3.Conn, commentLimit int, verbose bool) wordConfig {

	fmt.Printf("Preparing to query comments...\n")

	commentLimitPostfix := ""
	if commentLimit > 0 {
		commentLimitPostfix = "LIMIT " + strconv.Itoa(commentLimit)
	}

	stmt, err := conn.Prepare(
		"SELECT Content FROM CommentsContent " +
			"INNER JOIN temp.Scores ON(CommentsContent.rowid = temp.Scores.CommentId) " +
			"ORDER BY temp.Scores.Score DESC " +
			commentLimitPostfix)
	check(err, "Failed to select ranked comments")

	defer stmt.Close()

	fmt.Printf("Loading comments...\n")

	progressTime := time.Now()
	progressIteration := 0
	var comments []string

	for {
		hasRows, _ := stmt.Step()

		if !hasRows {
			break
		}

		var comment string
		_ = stmt.Scan(&comment)

		if verbose && len(comment) == 0 {
			fmt.Printf("ERROR: Empty comment detected!\n")
		}

		comments = append(comments, comment)

		progressIteration++
		if progressIteration%1000 == 0 && time.Since(progressTime).Seconds() > 2 {
			progressPerSeconds := float64(progressIteration) / time.Since(progressTime).Seconds()
			fmt.Printf("Loaded %d comments. %0.1f per sec.\n", len(comments), progressPerSeconds)

			progressTime = time.Now()
			progressIteration = 0
		}
	}

	fmt.Printf("Total comments loaded: %d\n", len(comments))

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
				currentInfo.score++
			}

			wordMap[key] = currentWordIds
		}

		//orderedWordKeys = append(orderedWordKeys, key)
	}

	progressTime = time.Now()
	totalComments := len(comments)
	progressIteration = 0

	for i := 0; i < totalComments; i++ {

		progressIteration++
		if progressIteration%1000 == 0 && time.Since(progressTime).Seconds() > 2 {

			progress := float64(i) / float64(totalComments) * 100.0

			fmt.Printf("Analyzed %d of %d (%.01f%%)\n", i, totalComments, progress)

			progressTime = time.Now()
			progressIteration = 0
		}

		tokens := reFindWords.FindAllString(comments[i], -1)

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
	outwordConfig.WordScores = make(map[int][]int)

	progressTime = time.Now()
	progressIteration = 0

	wordKeyIndex := 0
	for currentWordKey, currentWordInfos := range wordMap {

		if len(currentWordInfos) == 1 {
			outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfos[0].wordId)
			outwordConfig.WordScores[wordKeyIndex] = append(outwordConfig.WordScores[wordKeyIndex], currentWordInfos[0].score)
		} else {
			// Sort Descending (High Scores first)
			sort.Slice(currentWordInfos, func(i, j int) bool {
				return currentWordInfos[i].score > currentWordInfos[j].score
			})

			maxScore := currentWordInfos[0].score

			// for i := 1; i < len(currentWordInfos); i++ {
			// 	if currentWordInfos[i].score > maxScore {
			// 		maxScore += currentWordInfos[i].score
			// 	}
			// }

			// medianScore := math.Max(float64(maxScore)/2.0, 1.0)

			if maxScore == 1 {
				sort.Slice(currentWordInfos, func(i, j int) bool {
					return len(wordList[currentWordInfos[i].wordId]) > len(wordList[currentWordInfos[j].wordId])
				})
			}

			for i, currentWordInfo := range currentWordInfos {

				if i >= 3 {
					if maxScore == 1 {
						break
					}

					if maxScore >= 10 && currentWordInfo.score < 10 {
						break
					}

					if currentWordInfo.score <= 2 {
						break
					}
				}

				outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
				outwordConfig.WordScores[wordKeyIndex] = append(outwordConfig.WordScores[wordKeyIndex], currentWordInfo.score)
			}
		}

		/*
			if len(currentWordInfos) <= 3 {
				for _, currentWordInfo := range currentWordInfos {
					outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
					outwordConfig.WordScores[wordKeyIndex] = append(outwordConfig.WordScores[wordKeyIndex], currentWordInfo.score)
				}
			} else {
				topCount1 := 0
				topCount2 := 0
				topCount3 := 0

				for _, currentWordInfo := range currentWordInfos {
					if currentWordInfo.score > topCount1 {
						topCount3 = topCount2
						topCount2 = topCount1
						topCount1 = currentWordInfo.score
					} else if currentWordInfo.score > topCount2 {
						topCount3 = topCount2
						topCount2 = currentWordInfo.score
					} else if currentWordInfo.score > topCount3 {
						topCount3 = currentWordInfo.score
					}
				}

				for _, currentWordInfo := range currentWordInfos {
					if currentWordInfo.score == topCount3 || currentWordInfo.score == topCount2 || currentWordInfo.score == topCount1 {
						outwordConfig.WordMap[wordKeyIndex] = append(outwordConfig.WordMap[wordKeyIndex], currentWordInfo.wordId)
						outwordConfig.WordScores[wordKeyIndex] = append(outwordConfig.WordScores[wordKeyIndex], currentWordInfo.score)
					}
				}
			}
		*/

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

		progressIteration++
		if progressIteration%1000 == 0 && time.Since(progressTime).Seconds() > 2 {
			progress := float64(wordKeyIndex) / float64(len(wordMap)) * 100.0

			fmt.Printf("Prepared %d word mappings (%0.1f%%)\n", wordKeyIndex, progress)

			progressTime = time.Now()
			progressIteration = 0
		}
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
