package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"trebek/internal/config"
	"trebek/internal/game"
	"trebek/internal/irc"
	"trebek/internal/question"
)

func main() {
	// Initialize slog logger
	var logOutput *os.File
	var err error

	// Define command-line flags
	configPath := flag.String("c", "config.txt", "Path to the configuration file")
	botNameFlag := flag.String("botname", "", "Bot's name (overrides config file and env)")
	ircServerFlag := flag.String("ircserver", "", "IRC server address (overrides config file and env)")
	ircServerTLSFlag := flag.String("ircservertls", "", "IRC TLS server address (overrides config file and env)")
	ircChannelFlag := flag.String("ircchannel", "", "IRC channel to join (overrides config file and env)")
	logFilePathFlag := flag.String("logfile", "", "Path to log file (overrides config file and env)")
	logLevelFlag := flag.String("loglevel", "", "Log level (debug, info, warn, error) (overrides config file and env)")

	flag.Parse()

	configFilePath := *configPath

	// Ensure config.txt exists, create with defaults if not
	// This part remains, but the values will be overridden by flags/env vars later.
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		slog.Info("config.txt not found, creating with default values.")
		defaultConfigContent := `BOT_NAME=TrebekBot
IRC_SERVER=localhost:6667
IRC_SERVER_TLS=localhost:6697
IRC_CHANNEL=#
# LOG_FILE_PATH=/path/to/your/logfile.log
# LOG_LEVEL=info # debug, info, warn, error
`
		err := os.WriteFile(configFilePath, []byte(defaultConfigContent), 0600)
		if err != nil {
			slog.Default().Error("Failed to create default config.txt", "error", err)
			os.Exit(1)
		}
	}

	// Load configuration
	cfg, err := config.LoadConfig(
		configFilePath,
		*botNameFlag,
		*ircServerFlag,
		*ircServerTLSFlag,
		*ircChannelFlag,
		*logFilePathFlag,
		*logLevelFlag,
	)
	if err != nil {
		slog.Default().Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	if cfg.LogFilePath != "" {
		logOutput, err = os.OpenFile(cfg.LogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			slog.Default().Error("Failed to open log file", "path", cfg.LogFilePath, "error", err)
			os.Exit(1)
		}
		defer logOutput.Close()
	} else {
		logOutput = os.Stdout
	}

	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to Info level
	}

	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	// Load questions
	questionSource, err := question.NewJSONQuestionSource()
	if err != nil {
		slog.Error("Failed to create question source", "error", err)
		os.Exit(1)
	}
	defer questionSource.Close() // Ensure the question source is closed

	// Initialize game
	triviaGame := game.NewGame(questionSource, cfg.IRCChannel)

	// Create IRC client
	ircClient := irc.NewClient(cfg)

	// Game loop control
	gameStopChan := make(chan struct{})

	// Set up message handler
	ircClient.Handler = func(target, user, message string) {
		slog.Info("Message received", "user", user, "target", target, "message", message)
		msgLower := strings.ToLower(message)

		// Handle commands first
		if strings.HasPrefix(message, "!") {
			switch {
			case strings.HasPrefix(msgLower, "!hello"):
				ircClient.Privmsg(target, fmt.Sprintf("Hello, %s!", user))
			case strings.HasPrefix(msgLower, "!start"):
				if triviaGame.GetPlaying() {
					ircClient.Privmsg(target, "Trivia is already running!")
					return
				}
				triviaGame.SetPlaying(true)
				ircClient.Privmsg(target, "Starting continuous trivia!")
				go gameLoop(ircClient, triviaGame, gameStopChan)
				askQuestion(ircClient, triviaGame) // Ask the first question immediately
			case strings.HasPrefix(msgLower, "!stop"):
				if !triviaGame.GetPlaying() {
					ircClient.Privmsg(target, "Trivia is not currently running.")
					return
				}
				triviaGame.SetPlaying(false)
				close(gameStopChan) // Signal game loop to stop
				ircClient.Privmsg(target, "Stopping continuous trivia.")
			case strings.HasPrefix(msgLower, "!question"):
				if triviaGame.GetPlaying() {
					ircClient.Privmsg(target, "Trivia is running continuously. Please use !stop to end continuous play if you want to ask questions manually.")
					return
				}
				askQuestion(ircClient, triviaGame)
			case strings.HasPrefix(msgLower, "!answer "): // Keep !answer for explicit answers
				if triviaGame.GetCurrentQuestion() == nil {
					ircClient.Privmsg(target, "No question is currently active. Type !question to get one.")
					return
				}
				answerAttempt := strings.TrimPrefix(message, "!answer ")
				handleAnswer(ircClient, triviaGame, user, target, answerAttempt)
			case strings.HasPrefix(msgLower, "!hint"):
				hint, given := triviaGame.GetHint()
				if given {
					ircClient.Privmsg(target, fmt.Sprintf("Hint for %s: %s", triviaGame.GetCurrentQuestion().Category, hint))
					triviaGame.Scoreboard.AddScore(user, -game.HintCost) // Subtract points for hint
				} else {
					ircClient.Privmsg(target, hint) // Error message from GetHint
				}
			case strings.HasPrefix(msgLower, "!score"):
				score := triviaGame.Scoreboard.GetScore(user)
				ircClient.Privmsg(target, fmt.Sprintf("%s's score: %d", user, score))
			case strings.HasPrefix(msgLower, "!topscores"):
				scores := triviaGame.Scoreboard.Scores
				if len(scores) == 0 {
					ircClient.Privmsg(target, "No scores yet!")
					return
				}
				// Sort scores (simple bubble sort for few entries, or use sort.Slice)
				type player struct {
					name  string
					score int
				}
				var players []player
				for name, s := range scores {
					players = append(players, player{name, s})
				}
				for i := 0; i < len(players); i++ {
					for j := i + 1; j < len(players); j++ {
						if players[i].score < players[j].score {
							players[i], players[j] = players[j], players[i]
						}
					}
				}
				response := "Top Scores: "
				for i, p := range players {
					if i >= 5 { // Top 5
						break
					}
					response += fmt.Sprintf("%s: %d ", p.name, p.score)
				}
				ircClient.Privmsg(target, response)
			case strings.HasPrefix(msgLower, "!resetscoreboard"):
				triviaGame.Scoreboard.Reset()
				ircClient.Privmsg(target, "Scoreboard has been reset!")
			case strings.HasPrefix(msgLower, "!skip"):
				if triviaGame.GetCurrentQuestion() == nil {
					ircClient.Privmsg(target, "No question is currently active to skip.")
					return
				}
				currentVotes, threshold, skipped := triviaGame.AddNextVote(user)
				if skipped {
					ircClient.Privmsg(target, fmt.Sprintf("Question skipped! The answer was: %s", triviaGame.GetCurrentQuestion().Answer))
					triviaGame.ClearCurrentQuestion()
					if triviaGame.GetPlaying() {
						triviaGame.AnswerGiven <- false // Signal to game loop to get next question
					}
				} else {
					ircClient.Privmsg(target, fmt.Sprintf("%s voted to skip. %d/%d votes to skip.", user, currentVotes, threshold))
				}
			case strings.HasPrefix(msgLower, "!help"):
				ircClient.Privmsg(target, "Commands: !start, !stop, !question, !answer <your answer>, !hint, !score, !topscores, !resetscoreboard, !skip, !help")
			default:
				// Unknown command
				ircClient.Privmsg(target, fmt.Sprintf("Unknown command: %s. Type !help for commands.", message))
			}
		} else if triviaGame.GetPlaying() && triviaGame.GetCurrentQuestion() != nil {
			// If in continuous play and a question is active, treat non-command messages as answers
			handleAnswer(ircClient, triviaGame, user, target, message)
		}
	}

	// Connect to IRC
	useTLS := false
	serverToConnect := cfg.IRCServer
	if cfg.IRCServerTLS != "" {
		useTLS = true
		serverToConnect = cfg.IRCServerTLS
	}

	if serverToConnect == "" {
		slog.Error("No IRC server configured (neither IRC_SERVER nor IRC_SERVER_TLS is set).")
		os.Exit(1)
	}

	err = ircClient.Connect(useTLS)
	if err != nil {
		slog.Error("Failed to connect to IRC", "error", err)
		os.Exit(1)
	}
	defer ircClient.Close()

	// Join channel after a short delay to allow for server registration
	time.Sleep(2 * time.Second)
	ircClient.JoinChannel(cfg.IRCChannel)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go ircClient.Listen() // Start listening in a goroutine

	slog.Info("Trebek bot started. Waiting for messages...")
	<-sigChan // Block until a signal is received
	slog.Info("Shutting down bot...")
}

func askQuestion(ircClient *irc.Client, triviaGame *game.Game) {
	q := triviaGame.StartRound()
	if q == nil {
		ircClient.Privmsg(triviaGame.GameChannel, "No more questions left! Reset the game or load more questions.")
		triviaGame.SetPlaying(false) // Stop continuous play if no questions left
		return
	}
	ircClient.Privmsg(triviaGame.GameChannel, fmt.Sprintf("Category: %s - Question: %s", q.Category, q.Question))

	// Start a timer for the question
	triviaGame.QuestionTimer = time.AfterFunc(30*time.Second, func() {
		if triviaGame.GetCurrentQuestion() != nil { // If still unanswered
			ircClient.Privmsg(triviaGame.GameChannel, fmt.Sprintf("Time's up! The answer was: %s", triviaGame.GetCurrentQuestion().Answer))
			triviaGame.ClearCurrentQuestion()
			if triviaGame.GetPlaying() {
				triviaGame.AnswerGiven <- false // Signal that time ran out
			}
		}
	})
}

func handleAnswer(ircClient *irc.Client, triviaGame *game.Game, user, target, answerAttempt string) {
	if triviaGame.CheckAnswer(answerAttempt) {
		ircClient.Privmsg(target, fmt.Sprintf("Correct, %s! The answer was: %s", user, triviaGame.GetCurrentQuestion().Answer))
		triviaGame.Scoreboard.AddScore(user, 1) // Award 1 point for correct answer
		triviaGame.ClearCurrentQuestion()
		if triviaGame.GetPlaying() {
			triviaGame.AnswerGiven <- true // Signal that an answer was given
		}
	} else {
		ircClient.Privmsg(target, fmt.Sprintf("Sorry, %s, that's not correct.", user))
	}
}

func gameLoop(ircClient *irc.Client, triviaGame *game.Game, stopChan <-chan struct{}) {
	// Initial delay before asking the next question after an answer or skip
	// This ensures there's a brief pause before the next question appears.
	nextQuestionDelay := 5 * time.Second
	nextQuestionTimer := time.NewTimer(nextQuestionDelay)
	defer nextQuestionTimer.Stop()

	// Stop the initial timer immediately, as we'll manage it manually
	nextQuestionTimer.Stop()

	for {
		select {
		case <-nextQuestionTimer.C:
			// Time for the next question
			if triviaGame.GetPlaying() {
				askQuestion(ircClient, triviaGame)
				// Reset timer for the next question after this one is asked
				nextQuestionTimer.Reset(45 * time.Second) // Default interval for next question
			}
		case answered := <-triviaGame.AnswerGiven:
			// An answer was given or question timed out/skipped
			if triviaGame.GetPlaying() {
				if answered {
					// If answered correctly, ask next question after a short delay
					nextQuestionTimer.Reset(nextQuestionDelay)
				} else {
					// If timed out or skipped, ask next question after a short delay
					nextQuestionTimer.Reset(nextQuestionDelay)
				}
			}
		case <-stopChan:
			slog.Info("Game loop stopped.")
			return
		}
	}
}
