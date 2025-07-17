package game

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"trebek/internal/question"
)

var scoreboardFile = "scoreboard.json"

// regNonAlphaNum is a regex to match any character that is not a letter or a number.
var regNonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// Scoreboard stores player scores.
type Scoreboard struct {
	Scores map[string]int `json:"scores"`
	mu     sync.Mutex
}

// NewScoreboard creates a new scoreboard, loading from file if it exists.
func NewScoreboard() *Scoreboard {
	sb := &Scoreboard{
		Scores: make(map[string]int),
	}
	sb.load()
	return sb
}

// AddScore adds points to a player's score.
func (sb *Scoreboard) AddScore(player string, points int) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.Scores[player] += points
	sb.save()
}

// GetScore gets a player's score.
func (sb *Scoreboard) GetScore(player string) int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.Scores[player]
}

// Reset resets the scoreboard.
func (sb *Scoreboard) Reset() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.Scores = make(map[string]int)
	sb.save()
}

// save saves the scoreboard to a file.
func (sb *Scoreboard) save() {
	bytes, err := json.MarshalIndent(sb.Scores, "", "  ")
	if err != nil {
		log.Printf("Error marshalling scoreboard: %v", err)
		return
	}
	err = os.WriteFile(scoreboardFile, bytes, 0600)
	if err != nil {
		log.Printf("Error saving scoreboard: %v", err)
	}
}

// load loads the scoreboard from a file.
func (sb *Scoreboard) load() {
	bytes, err := os.ReadFile(scoreboardFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Scoreboard file %s not found, starting new.", scoreboardFile)
			return
		}
		log.Printf("Error reading scoreboard file: %v", err)
		return
	}
	err = json.Unmarshal(bytes, &sb.Scores)
	if err != nil {
		log.Printf("Error unmarshalling scoreboard: %v", err)
	}
}

// Game represents the trivia game state.
const (
	HintCost = 50 // Points subtracted per hint
	MaxHints = 3  // Maximum hints per question
)

// Game represents the trivia game state.
type Game struct {
	questionSource    question.QuestionSource // Source for new questions
	questionBuffer    []*question.Question    // Buffer of upcoming questions
	bufferMu          sync.Mutex              // Mutex for questionBuffer
	CurrentQuestion   *question.Question
	Scoreboard        *Scoreboard
	mu                sync.Mutex
	rand              *rand.Rand
	hintCount         int    // Number of hints given for the current question
	hintMask          []rune // Current state of the masked hint
	IsPlaying         bool   // True if continuous trivia is active
	GameChannel       string // The IRC channel where the game is played
	QuestionTimer     *time.Timer
	AnswerGiven       chan bool       // Channel to signal that an answer was given
	nextVotes         map[string]bool // Users who voted to skip
	NextVoteThreshold int             // Number of votes required to skip
}

// NewGame creates a new game instance.
func NewGame(qs question.QuestionSource, channel string) *Game {
	source := rand.NewSource(time.Now().UnixNano())
	g := &Game{
		questionSource:    qs,
		questionBuffer:    make([]*question.Question, 0, 3), // Initialize buffer with capacity
		Scoreboard:        NewScoreboard(),
		rand:              rand.New(source), // #nosec G404
		hintMask:          []rune{},
		IsPlaying:         false,
		GameChannel:       channel,
		AnswerGiven:       make(chan bool),
		nextVotes:         make(map[string]bool),
		NextVoteThreshold: 3, // Default: 3 votes to skip
	}
	g.fillQuestionBuffer() // Fill buffer initially
	return g
}

// fillQuestionBufferUnlocked replenishes the question buffer up to a certain size without acquiring a lock.
// It must be called with g.bufferMu already held.
func (g *Game) fillQuestionBufferUnlocked() {
	for len(g.questionBuffer) < 3 { // Keep at least 3 questions in buffer
		q, err := g.questionSource.Next()
		if err == io.EOF {
			// No more questions from source
			break
		}
		if err != nil {
			log.Printf("Error fetching next question: %v", err)
			break
		}
		g.questionBuffer = append(g.questionBuffer, q)
	}
}

// fillQuestionBuffer replenishes the question buffer up to a certain size.
func (g *Game) fillQuestionBuffer() {
	g.bufferMu.Lock()
	defer g.bufferMu.Unlock()
	g.fillQuestionBufferUnlocked()
}

// StartRound selects a new question and returns it.
func (g *Game) StartRound() *question.Question {
	g.mu.Lock()
	defer g.mu.Unlock()

	var q *question.Question

	// Acquire bufferMu to safely access and modify questionBuffer
	g.bufferMu.Lock()
	if len(g.questionBuffer) == 0 {
		// If buffer is empty, fill it synchronously to ensure we get a question if available
		g.fillQuestionBufferUnlocked() // Call the unlocked version
	}

	if len(g.questionBuffer) == 0 {
		g.bufferMu.Unlock() // Release lock before returning nil
		return nil          // No questions left
	}

	// Pick a random question from the buffer
	idx := g.rand.Intn(len(g.questionBuffer))
	q = g.questionBuffer[idx]

	// Remove the question from the buffer
	g.questionBuffer = append(g.questionBuffer[:idx], g.questionBuffer[idx+1:]...)

	g.bufferMu.Unlock() // Release lock before launching goroutine

	g.CurrentQuestion = q

	// Replenish buffer in a separate goroutine after question is taken
	// This ensures the main thread isn't blocked and the buffer is topped up for next round
	go g.fillQuestionBuffer() // This calls the locked version

	return g.CurrentQuestion
}

// CheckAnswer checks if the given answer is correct.
func (g *Game) CheckAnswer(answer string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.CurrentQuestion == nil {
		return false
	}

	// Normalize both the provided answer and the correct answer for comparison.
	// This includes converting to lowercase, trimming spaces, and removing non-alphanumeric characters.
	normalizedAttempt := normalizeAnswer(answer)
	normalizedCorrect := normalizeAnswer(g.CurrentQuestion.Answer)

	// Perform a simple equality check after normalization.
	return normalizedAttempt == normalizedCorrect
}

// normalizeAnswer converts the input string to lowercase, trims spaces, and removes
// all non-alphanumeric characters. This helps in robust answer matching.
func normalizeAnswer(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	s = regNonAlphaNum.ReplaceAllString(s, "")
	return s
}

// GetCurrentQuestion returns the current question.
func (g *Game) GetCurrentQuestion() *question.Question {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.CurrentQuestion
}

// ClearCurrentQuestion clears the current question after it's answered or timed out.
func (g *Game) ClearCurrentQuestion() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.CurrentQuestion = nil
	g.hintCount = 0
	g.hintMask = []rune{}
	g.nextVotes = make(map[string]bool) // Reset votes for new question
	if g.QuestionTimer != nil {
		g.QuestionTimer.Stop()
	}
}

// SetPlaying sets the game's playing state.
func (g *Game) SetPlaying(playing bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.IsPlaying = playing
}

// GetPlaying returns the game's playing state.
func (g *Game) GetPlaying() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.IsPlaying
}

// GetHint generates and returns a hint for the current question.
// It returns the hint string and a boolean indicating if a hint was given.
func (g *Game) GetHint() (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.CurrentQuestion == nil {
		return "No question is currently active.", false
	}
	if g.hintCount >= MaxHints {
		return fmt.Sprintf("Maximum hints (%d) reached for this question.", MaxHints), false
	}

	answer := []rune(strings.TrimSpace(g.CurrentQuestion.Answer))
	if len(g.hintMask) == 0 {
		g.hintMask = make([]rune, len(answer))
		for i, r := range answer {
			if r == ' ' {
				g.hintMask[i] = ' '
			} else {
				g.hintMask[i] = '_'
			}
		}
	}

	// Reveal a character
	revealed := false
	for i := 0; i < 5 && !revealed; i++ { // Try up to 5 times to find a character to reveal
		idx := g.rand.Intn(len(answer))
		if g.hintMask[idx] == '_' {
			g.hintMask[idx] = answer[idx]
			revealed = true
		}
	}

	if !revealed { // If no new character was revealed (e.g., all revealed or only spaces left)
		for i, r := range g.hintMask { // Fallback: reveal first available underscore
			if r == '_' {
				g.hintMask[i] = answer[i]
				revealed = true
				break
			}
		}
	}

	if revealed {
		g.hintCount++
		return string(g.hintMask), true
	}
	return "No more characters to reveal for a hint.", false
}

// AddNextVote adds a vote to skip the current question.
// Returns true if the question should be skipped.
func (g *Game) AddNextVote(user string) (int, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.CurrentQuestion == nil {
		return 0, 0, false // No question to skip
	}

	if _, exists := g.nextVotes[user]; exists {
		return len(g.nextVotes), g.NextVoteThreshold, false // User already voted
	}

	g.nextVotes[user] = true
	currentVotes := len(g.nextVotes)

	if currentVotes >= g.NextVoteThreshold {
		return currentVotes, g.NextVoteThreshold, true // Threshold reached
	}
	return currentVotes, g.NextVoteThreshold, false
}
