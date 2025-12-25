# doofus-rick

Our Discord bot. Stores quotes and serves them on a website.

## Installation

1. Install Go 1.25+
2. Run `go mod download`

You can run the application either through `go run .` or, if you want hot-reloading for web development,
use [air](https://github.com/air-verse/air):

```shell
air
```

## Config and environment

The application is configured via environment variables:

| Variable                | Description                            | Default     |
|:------------------------|:---------------------------------------|:------------|
| `DISCORD_TOKEN`         | Discord Bot Token                      |             |
| `DISCORD_GUILD`         | Private Guild ID for commands and auth |             |
| `DISCORD_CLIENT_ID`     | OAuth2 Client ID                       |             |
| `DISCORD_CLIENT_SECRET` | OAuth2 Client Secret                   |             |
| `DISCORD_REDIRECT_URI`  | OAuth2 Callback URL                    |             |
| `DB_HOST`               | PostgreSQL Host                        | `localhost` |
| `DB_USER`               | PostgreSQL User                        | `postgres`  |
| `DB_PASS`               | PostgreSQL Password                    |             |
| `DB_NAME`               | PostgreSQL Database Name               | `postgres`  |
| `DB_PORT`               | PostgreSQL Port                        | `5432`      |
| `PORT`                  | Web Server Port                        | `:8080`     |
| `SESSION_SECRET`        | Secret key for signed cookies          |             |
| `APP_ENV`               | Set to 'production' to ignore .env     |             |

## License

[MIT](/LICENSE)
