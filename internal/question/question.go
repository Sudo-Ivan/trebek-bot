package question

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
)

//go:embed all.json
var content embed.FS

// Question represents a single trivia question from the JSON data.
type Question struct {
	Category string `json:"category"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Money    string `json:"money"`
	Date     string `json:"date"`
	Episode  int    `json:"episode"`
}

// QuestionSource defines an interface for fetching questions.
type QuestionSource interface {
	Next() (*Question, error)
	Close() error
}

// jsonQuestionSource implements QuestionSource for JSON files.
type jsonQuestionSource struct {
	decoder *json.Decoder
	file    io.ReadCloser // To close the underlying file/reader
}

// NewJSONQuestionSource creates a new jsonQuestionSource.
func NewJSONQuestionSource() (QuestionSource, error) {
	f, err := content.Open("all.json")
	if err != nil {
		return nil, err
	}
	// Assume all.json is an array of questions
	decoder := json.NewDecoder(f)

	// Read the opening bracket of the JSON array
	if t, err := decoder.Token(); err != nil || t != json.Delim('[') {
		if closeErr := f.Close(); closeErr != nil {
			// Log the close error, but prioritize the original error
			// In a real application, you might use a logger here.
			fmt.Printf("Error closing file after initial token read failure: %v\n", closeErr)
		}
		return nil, fmt.Errorf("expected JSON array start, got %v: %w", t, err)
	}

	return &jsonQuestionSource{
		decoder: decoder,
		file:    f,
	}, nil
}

// Next fetches the next question from the JSON source.
func (jqs *jsonQuestionSource) Next() (*Question, error) {
	if !jqs.decoder.More() {
		// No more elements in the array
		return nil, io.EOF
	}

	var q Question
	if err := jqs.decoder.Decode(&q); err != nil {
		return nil, err
	}
	return &q, nil
}

// Close closes the underlying file/reader.
func (jqs *jsonQuestionSource) Close() error {
	// Read the closing bracket of the JSON array
	if t, err := jqs.decoder.Token(); err != nil || t != json.Delim(']') {
		// If there's an error or it's not the closing delimiter, it might be an incomplete read.
		// Still try to close the file.
		return fmt.Errorf("error reading closing JSON array delimiter: %w", err)
	}
	return jqs.file.Close()
}

// LoadQuestions is now deprecated. Use NewJSONQuestionSource instead.
// It's kept for compatibility until all callers are updated.
func LoadQuestions() ([]Question, error) {
	bytes, err := content.ReadFile("all.json")
	if err != nil {
		return nil, err
	}

	var questions []Question
	if err := json.Unmarshal(bytes, &questions); err != nil {
		return nil, err
	}

	return questions, nil
}
