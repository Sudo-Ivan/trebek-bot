# Trebek Trivia Bot (Go)

[![Go Tests](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/test.yml/badge.svg)](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/test.yml)
[![Go Build Multi-Platform](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/build.yml/badge.svg)](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/build.yml)
[![Run Gosec](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/gosec.yml/badge.svg)](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/gosec.yml)
[![Build and Publish Docker Image](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/docker.yml/badge.svg)](https://github.com/Sudo-Ivan/trebek-bot/actions/workflows/docker.yml)


A simple and modern Trebek trivia bot built in Go for IRC servers. Inspired by [original ruby bot](https://github.com/epitron/trebek).

- No 3rd-party Go dependencies
- Low cpu/mem footprint
- Fast
- Docker/Podman Ready

Tested with Ergo IRC Server

## Setup and Run

### 1. Download Pre-built Binary (Recommended)

The easiest way to get started is to download a pre-built binary from the [releases page](https://github.com/Sudo-Ivan/trebek-bot/releases). Choose the appropriate binary for your operating system and architecture.

This will install the `trebek` executable in your `$GOPATH/bin` (or `$GOBIN`) directory.

### 2. Manual Build and Run

If you prefer to build from source or want to make modifications:

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/Sudo-Ivan/trebek-bot.git
    cd trebek-bot
    ```
2.  **Build the executable:**
    ```bash
    go build -o trebek ./cmd/trebek
    ```
3.  **Run the bot:**
    ```bash
    ./trebek
    ```

### 3. Docker/Podman

Amd64 and Arm64 supported.

```bash
docker pull ghcr.io/sudo-ivan/trebek-bot:latest

# To run the Trebek bot with network host mode (required to connect to IRC servers running on localhost):
docker run --rm -d --network host -v ./config.txt:/app/config.txt --name trebek-bot ghcr.io/sudo-ivan/trebek-bot:latest

# To run the Trebek bot without network host mode (default Docker networking):
docker run --rm -d -v ./config.txt:/app/config.txt --name trebek-bot ghcr.io/sudo-ivan/trebek-bot:latest
```

**Build and Run Docker Image Manually:**

```bash
docker build -t trebek-bot .
docker run --name my-trebek-bot trebek-bot
```

Remember to configure your `config.txt` or environment variables as needed.

## Tests

Currently there are tests for `game.go` and `client.go`

```bash
go test ./... -v
```

## Architecture and Question Management

The Trebek bot is designed with a modular architecture to separate concerns:

*   **IRC Client (`internal/irc`):** Handles communication with the IRC server, including connecting, joining channels, sending messages, and processing incoming messages.
*   **Game Logic (`internal/game`):** Manages the trivia game state, including current question, scoreboard, hints, and game flow.
*   **Question Management (`internal/question`):** Responsible for providing trivia questions to the game logic.

### Question Loading Mechanism

To optimize memory usage and handle potentially large question datasets, the bot employs an on-demand question loading mechanism:

1.  **`QuestionSource` Interface:** The `internal/question` package defines a `QuestionSource` interface. This allows for flexible question providers.
2.  **`jsonQuestionSource` Implementation:** The `jsonQuestionSource` reads questions from the `all.json` file using `json.Decoder`. Crucially, it does not load the entire file into memory. Instead, it streams questions one by one as they are requested.
3.  **Question Buffer in Game Logic:** The `internal/game` package maintains a small buffer (e.g., 3 questions) of upcoming questions. When a question is needed, it's taken from this buffer. A background goroutine then replenishes the buffer from the `QuestionSource`, ensuring that questions are always available without consuming excessive memory. This approach balances responsiveness with memory efficiency.

## License

[MIT](LICENSE)
