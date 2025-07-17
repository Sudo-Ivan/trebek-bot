package game

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"trebek/internal/question"
)

// mockQuestionSource for testing
type mockQuestionSource struct {
	questions []*question.Question
	index     int
}

func newMockQuestionSource(q []*question.Question) *mockQuestionSource {
	return &mockQuestionSource{questions: q}
}

func (m *mockQuestionSource) Next() (*question.Question, error) {
	if m.index >= len(m.questions) {
		return nil, io.EOF
	}
	q := m.questions[m.index]
	m.index++
	return q, nil
}

func (m *mockQuestionSource) Close() error {
	return nil
}

// Helper function to create a temporary scoreboard file for testing
func createTempScoreboardFile(t *testing.T, content string) string {
	tmpfile, err := os.CreateTemp("", "scoreboard_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if content == "" {
		content = "{}" // Initialize with empty JSON object for valid unmarshalling
	}
	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	return tmpfile.Name()
}

func TestNewScoreboard(t *testing.T) {
	// Test with no existing file
	tempFile := createTempScoreboardFile(t, "")
	defer os.Remove(tempFile) // Clean up

	originalScoreboardFile := scoreboardFile
	scoreboardFile = tempFile
	defer func() { scoreboardFile = originalScoreboardFile }()

	sb := NewScoreboard()
	if sb == nil {
		t.Fatal("NewScoreboard returned nil")
	}
	if len(sb.Scores) != 0 {
		t.Errorf("Expected empty scoreboard, got %v", sb.Scores)
	}

	// Test with existing file
	existingContent := `{"player1": 10, "player2": 20}`
	tempFileWithContent := createTempScoreboardFile(t, existingContent)
	defer os.Remove(tempFileWithContent)

	scoreboardFile = tempFileWithContent
	sb = NewScoreboard()
	if sb == nil {
		t.Fatal("NewScoreboard returned nil for existing file")
	}
	if sb.Scores["player1"] != 10 || sb.Scores["player2"] != 20 {
		t.Errorf("Expected scores to be loaded, got %v", sb.Scores)
	}
}

func TestScoreboardAddGetReset(t *testing.T) {
	tempFile := createTempScoreboardFile(t, "")
	defer os.Remove(tempFile)
	originalScoreboardFile := scoreboardFile
	scoreboardFile = tempFile
	defer func() { scoreboardFile = originalScoreboardFile }()

	sb := NewScoreboard()

	sb.AddScore("playerA", 100)
	if score := sb.GetScore("playerA"); score != 100 {
		t.Errorf("Expected score 100 for playerA, got %d", score)
	}

	sb.AddScore("playerA", 50)
	if score := sb.GetScore("playerA"); score != 150 {
		t.Errorf("Expected score 150 for playerA, got %d", score)
	}

	sb.AddScore("playerB", 75)
	if score := sb.GetScore("playerB"); score != 75 {
		t.Errorf("Expected score 75 for playerB, got %d", score)
	}

	sb.Reset()
	if len(sb.Scores) != 0 {
		t.Errorf("Expected empty scoreboard after reset, got %v", sb.Scores)
	}
	if score := sb.GetScore("playerA"); score != 0 {
		t.Errorf("Expected score 0 for playerA after reset, got %d", score)
	}
}

func TestScoreboardSaveLoad(t *testing.T) {
	tempFile := createTempScoreboardFile(t, "")
	defer os.Remove(tempFile)
	originalScoreboardFile := scoreboardFile
	scoreboardFile = tempFile
	defer func() { scoreboardFile = originalScoreboardFile }()

	sb := NewScoreboard()
	sb.AddScore("playerX", 10)
	sb.AddScore("playerY", 20)

	// Simulate saving and then loading into a new scoreboard instance
	sb2 := NewScoreboard() // This will load from the same temp file
	if sb2.Scores["playerX"] != 10 || sb2.Scores["playerY"] != 20 {
		t.Errorf("Scores not correctly loaded from file: %v", sb2.Scores)
	}
}

func TestNewGame(t *testing.T) {
	mockQs := newMockQuestionSource([]*question.Question{
		{Category: "Test", Question: "Q1", Answer: "A1"},
	})
	game := NewGame(mockQs, "#testchannel")

	if game == nil {
		t.Fatal("NewGame returned nil")
	}
	if len(game.questionBuffer) != 1 { // Check buffer size
		t.Errorf("Expected 1 question in buffer, got %d", len(game.questionBuffer))
	}
	if game.GameChannel != "#testchannel" {
		t.Errorf("Expected game channel #testchannel, got %s", game.GameChannel)
	}
	if game.Scoreboard == nil {
		t.Error("Scoreboard not initialized")
	}
	if game.rand == nil {
		t.Error("Random source not initialized")
	}
	if game.IsPlaying != false {
		t.Error("IsPlaying should be false initially")
	}
	if game.AnswerGiven == nil {
		t.Error("AnswerGiven channel not initialized")
	}
	if game.NextVoteThreshold != 3 {
		t.Errorf("Expected NextVoteThreshold 3, got %d", game.NextVoteThreshold)
	}
}

func TestStartRound(t *testing.T) {
	mockQs := newMockQuestionSource([]*question.Question{
		{Category: "Test", Question: "Q1", Answer: "A1"},
		{Category: "Test", Question: "Q2", Answer: "A2"},
		{Category: "Test", Question: "Q3", Answer: "A3"}, // Add a third for buffer testing
		{Category: "Test", Question: "Q4", Answer: "A4"},
	})
	game := NewGame(mockQs, "#testchannel")

	// Test starting a round
	currentQ := game.StartRound()
	if currentQ == nil {
		t.Fatal("StartRound returned nil")
	}
	if game.GetCurrentQuestion() == nil {
		t.Error("CurrentQuestion not set")
	}
	// The buffer should replenish, so its size might not be what you expect immediately
	// Instead, check if the question was indeed removed from the buffer
	found := false
	game.bufferMu.Lock()
	for _, q := range game.questionBuffer {
		if q == currentQ {
			found = true
			break
		}
	}
	game.bufferMu.Unlock()
	if found {
		t.Error("Current question should have been removed from buffer")
	}

	// Test starting another round
	currentQ2 := game.StartRound()
	if currentQ2 == nil {
		t.Fatal("StartRound returned nil for second question")
	}

	// Test starting a third round
	currentQ3 := game.StartRound()
	if currentQ3 == nil {
		t.Fatal("StartRound returned nil for third question")
	}

	// Test starting a fourth round
	currentQ4 := game.StartRound()
	if currentQ4 == nil {
		t.Fatal("StartRound returned nil for fourth question")
	}

	// Test no questions left (after exhausting mock source and buffer)
	// Need to ensure fillQuestionBuffer has run for all questions
	time.Sleep(500 * time.Millisecond) // Give goroutine time to run
	noQ := game.StartRound()
	if noQ != nil {
		t.Errorf("Expected nil when no questions left, got %v", noQ)
	}
}

func TestCheckAnswer(t *testing.T) {
	mockQs := newMockQuestionSource([]*question.Question{
		{Category: "Test", Question: "What is 2+2?", Answer: "4"},
	})
	game := NewGame(mockQs, "#testchannel")
	game.StartRound()

	tests := []struct {
		answer   string
		expected bool
	}{
		{"4", true},
		{" 4 ", true}, // Trim spaces
		{"4 ", true},
		{"4", true},
		{"four", false},
		{"", false},
		{"  ", false},
	}

	for _, test := range tests {
		if game.CheckAnswer(test.answer) != test.expected {
			t.Errorf("CheckAnswer(%q) expected %t, got %t", test.answer, test.expected, !test.expected)
		}
	}

	// Test with no current question
	game.ClearCurrentQuestion()
	if game.CheckAnswer("anything") != false {
		t.Error("CheckAnswer should be false when no question is active")
	}
}

func TestClearCurrentQuestion(t *testing.T) {
	mockQs := newMockQuestionSource([]*question.Question{
		{Category: "Test", Question: "Q1", Answer: "A1"},
	})
	game := NewGame(mockQs, "#testchannel")
	game.StartRound()

	game.hintCount = 2
	game.hintMask = []rune{'a', '_', 'c'}
	game.nextVotes["user1"] = true
	game.QuestionTimer = time.AfterFunc(time.Hour, func() {}) // Dummy timer

	game.ClearCurrentQuestion()

	if game.CurrentQuestion != nil {
		t.Error("CurrentQuestion not cleared")
	}
	if game.hintCount != 0 {
		t.Errorf("Hint count not reset, got %d", game.hintCount)
	}
	if len(game.hintMask) != 0 {
		t.Errorf("Hint mask not cleared, got %v", game.hintMask)
	}
	if len(game.nextVotes) != 0 {
		t.Errorf("Next votes not cleared, got %v", game.nextVotes)
	}
	if game.QuestionTimer != nil {
		game.QuestionTimer.Stop() // Ensure stop is called
	}
	// We don't assert on the return value of Stop() as it can be false if the timer already fired.
	// The important part is that the timer is no longer active.
}

func TestSetGetPlaying(t *testing.T) {
	mockQs := newMockQuestionSource([]*question.Question{})
	game := NewGame(mockQs, "#test")

	game.SetPlaying(true)
	if !game.GetPlaying() {
		t.Error("SetPlaying(true) failed")
	}

	game.SetPlaying(false)
	if game.GetPlaying() {
		t.Error("SetPlaying(false) failed")
	}
}

func TestGetHint(t *testing.T) {
	mockQs := newMockQuestionSource([]*question.Question{
		{Category: "Test", Question: "Capital of France?", Answer: "Paris"},
	})
	game := NewGame(mockQs, "#testchannel")
	game.StartRound()

	// Test first hint
	hint, given := game.GetHint()
	if !given {
		t.Error("Expected hint to be given")
	}
	if len(hint) != len("Paris") {
		t.Errorf("Hint length mismatch, expected %d, got %d", len("Paris"), len(hint))
	}
	if !strings.Contains(hint, "_") { // Should still have underscores
		t.Error("Hint should contain underscores")
	}
	if game.hintCount != 1 {
		t.Errorf("Expected hintCount 1, got %d", game.hintCount)
	}

	// Test subsequent hints
	for i := 0; i < MaxHints-1; i++ {
		_, given = game.GetHint()
		if !given {
			t.Errorf("Expected hint to be given on iteration %d", i+2)
		}
	}
	if game.hintCount != MaxHints {
		t.Errorf("Expected hintCount %d, got %d", MaxHints, game.hintCount)
	}

	// Test max hints reached
	hint, given = game.GetHint()
	if given {
		t.Error("Expected no hint to be given after max hints reached")
	}
	if !strings.Contains(hint, "Maximum hints") {
		t.Errorf("Expected max hints message, got %s", hint)
	}

	// Test no question active
	game.ClearCurrentQuestion()
	hint, given = game.GetHint()
	if given {
		t.Error("Expected no hint when no question active")
	}
	if !strings.Contains(hint, "No question is currently active") {
		t.Errorf("Expected no question active message, got %s", hint)
	}
}

func TestAddNextVote(t *testing.T) {
	// Test case 1: Basic voting and skipping
	t.Run("BasicVotingAndSkipping", func(t *testing.T) {
		mockQs := newMockQuestionSource([]*question.Question{
			{Category: "Test", Question: "Q1", Answer: "A1"},
			{Category: "Test", Question: "Q2", Answer: "A2"}, // Add another question
		})
		game := NewGame(mockQs, "#testchannel")
		game.StartRound()
		game.NextVoteThreshold = 2 // Set a low threshold for testing

		// First vote
		currentVotes, threshold, skipped := game.AddNextVote("user1")
		if skipped {
			t.Error("Expected not skipped on first vote")
		}
		if currentVotes != 1 || threshold != 2 {
			t.Errorf("Expected 1/2 votes, got %d/%d", currentVotes, threshold)
		}

		// Second vote (should skip)
		currentVotes, threshold, skipped = game.AddNextVote("user2")
		if !skipped {
			t.Error("Expected skipped on second vote")
		}
		if currentVotes != 2 || threshold != 2 {
			t.Errorf("Expected 2/2 votes, got %d/%d", currentVotes, threshold)
		}
		// Simulate game clearing the question after skip
		game.ClearCurrentQuestion()
	})

	// Test case 2: User already voted
	t.Run("UserAlreadyVoted", func(t *testing.T) {
		mockQs := newMockQuestionSource([]*question.Question{
			{Category: "Test", Question: "Q3", Answer: "A3"},
		})
		game := NewGame(mockQs, "#testchannel")
		game.StartRound() // Start a new question for this test case
		game.NextVoteThreshold = 2
		game.AddNextVote("userA")
		currentVotes, threshold, skipped := game.AddNextVote("userA")
		if skipped {
			t.Error("Expected not skipped when user votes again")
		}
		if currentVotes != 1 || threshold != 2 {
			t.Errorf("Expected 1/2 votes, got %d/%d", currentVotes, threshold)
		}
	})

	// Test case 3: No question active
	t.Run("NoQuestionActive", func(t *testing.T) {
		mockQs := newMockQuestionSource([]*question.Question{}) // No questions
		game := NewGame(mockQs, "#testchannel")
		game.ClearCurrentQuestion() // Ensure no question is active
		currentVotes, threshold, skipped := game.AddNextVote("userX")
		if skipped {
			t.Error("Expected not skipped when no question active")
		}
		if currentVotes != 0 || threshold != 0 {
			t.Errorf("Expected 0/0 votes, got %d/%d", currentVotes, threshold)
		}
	})
}
