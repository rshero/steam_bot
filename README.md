# Steam Deals Bot 🎮

A high-performance Telegram bot written in Go that searches for Steam games and posts the best game deals automatically.

## Features 🚀

- **Inline Search**: Search for any Steam game directly within Telegram (`@your_bot game_name`)
- **Deal Alerts**: Automatically posts top deals from CheapShark to a configured channel
- **Detailed Info**: View price history, regional pricing (INR), and system requirements
- **Fast & Efficient**: Built with Go for high concurrency and low resource usage

## Setup 🛠️

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd steam_bot
   ```

2. **Configure Environment**
   Create a `.env` file in the root directory:
   ```env
   BOT_TOKEN=your_telegram_bot_token
   CHANNEL_ID=your_channel_id
   ```

3. **Build & Run**
   ```bash
   go mod tidy
   go build -o steam_bot.exe (os specific)
   ./steam_bot.exe
   ```

## Usage 📱

- **Inline Query**: Type `@BotName <game name>` in any chat to search.
- **Deals**: The bot automatically checks for deals every hour and posts them to the channel specified in `CHANNEL_ID`.

## Credits 👏

- **Telegram Library**: [gotgbot](https://github.com/PaulSonOfLars/gotgbot)
- **Game Deals API**: [CheapShark](https://www.cheapshark.com/api/1.0/)
- **Game Data**: [Steam Store API](https://store.steampowered.com/)

## License

MIT
