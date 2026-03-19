# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Huobao Drama (火宝短剧) is a full-stack AI short drama automation platform that automates the workflow from script generation, character design, storyboarding to video composition using AI.

**Tech Stack:**
- **Backend**: Go 1.23+, Gin (HTTP), GORM (ORM), SQLite, Zap (logging)
- **Frontend**: Vue 3.4+, TypeScript, Vite 5, Element Plus, TailwindCSS, Pinia
- **Video Processing**: FFmpeg (required)
- **AI Providers**: OpenAI, Gemini, Doubao (火山引擎), MiniMax, Chatfire

## Development Commands

### Backend (Go)
```bash
# Run backend server
go run main.go

# Download dependencies
go mod download

# Build executable
go build -o huobao-drama .

# Data migration tool (for historical data)
go run cmd/migrate/main.go
```

### Frontend (Vue)
```bash
cd web

# Install dependencies
npm install

# Development server (hot reload, proxies to backend)
npm run dev

# Build for production
npm run build

# Type check + build
npm run build:check

# Lint
npm run lint
```

### Docker
```bash
# Build and run with docker-compose (recommended)
docker-compose up -d

# Build only
docker-compose build

# View logs
docker-compose logs -f
```

### Configuration
1. Copy config example: `cp configs/config.example.yaml configs/config.yaml`
2. Edit `configs/config.yaml` for your environment
3. Set `app.debug: true` for development, `false` for production

**Key config paths:**
- `configs/config.yaml` - Main configuration file
- `configs/config.example.yaml` - Configuration template

## Architecture

This project follows **DDD (Domain-Driven Design)** with clear layering:

```
├── api/                    # HTTP Layer
│   ├── handlers/           # Request handlers (controllers)
│   ├── middlewares/        # CORS, logging, rate limiting
│   └── routes/             # Route definitions (routes.go)
│
├── application/            # Application Layer
│   └── services/           # Business logic services
│
├── domain/                 # Domain Layer
│   └── models/             # Domain entities (Drama, Episode, Storyboard, etc.)
│
├── infrastructure/         # Infrastructure Layer
│   ├── database/           # DB connection and migrations
│   ├── external/           # External service integrations
│   ├── scheduler/          # Background job scheduling
│   └── storage/            # File storage (local)
│
├── pkg/                    # Shared utilities
│   ├── ai/                 # AI clients (OpenAI, Gemini)
│   ├── config/             # Configuration loading
│   ├── image/              # Image generation clients
│   ├── logger/             # Zap logger wrapper
│   ├── response/           # HTTP response helpers
│   ├── utils/              # General utilities
│   └── video/              # Video generation clients
│
├── web/                    # Frontend (Vue 3)
│   └── src/
│       ├── api/            # API client functions
│       ├── components/     # Reusable Vue components
│       ├── stores/         # Pinia state management
│       ├── views/          # Page components
│       └── router/         # Vue Router config
│
├── migrations/             # Database migration files
├── cmd/migrate/            # Data migration CLI tool
└── configs/                # Configuration files
```

## Core Domain Models

Located in `domain/models/`:

- **Drama** - Top-level project containing episodes, characters, scenes, props
- **Episode** - Single episode/chapter with script content and storyboards
- **Storyboard** - Individual shot with image/video prompts, dialogue, duration
- **Character** - Character with appearance, personality, image references
- **Scene** - Background/scene with location, time, image prompt
- **Prop** - Props with image generation prompts
- **Asset** - Media assets (images, videos) with local storage paths

## API Structure

All API routes are under `/api/v1/`. Key endpoints:

- `/api/v1/dramas` - Drama CRUD operations
- `/api/v1/dramas/:id/episodes` - Episode management
- `/api/v1/ai-configs` - AI provider configuration
- `/api/v1/characters` - Character management and image generation
- `/api/v1/scenes` - Scene management
- `/api/v1/storyboards` - Storyboard operations
- `/api/v1/images` - Image generation
- `/api/v1/videos` - Video generation
- `/api/v1/video-merges` - Video composition
- `/api/v1/tasks/:task_id` - Async task status polling

## Frontend Routes

- `/` - Drama list
- `/dramas/create` - Create new drama
- `/dramas/:id` - Drama management
- `/dramas/:id/episode/:episodeNumber` - Episode workflow
- `/episodes/:id/storyboard` - Storyboard editor
- `/episodes/:id/generate` - Image/video generation
- `/timeline/:id` - Timeline editor
- `/settings/ai-config` - AI configuration

## Important Patterns

### Database
- Uses GORM with auto-migration on startup (no manual migrations needed)
- SQLite is default (pure Go driver `modernc.org/sqlite`, supports `CGO_ENABLED=0`)
- Database file: `./data/drama_generator.db` (configurable)

### Storage
- Local file storage in `./data/storage/`
- Static files served at `/static` endpoint
- Generated media stored with local paths for caching

### AI Services
- AI configurations stored in database (`ai_configs` table), managed via Web UI
- Multiple providers supported: OpenAI, Gemini, Doubao, etc.
- Default providers configured in `config.yaml`

### Async Tasks
- Long-running operations (image/video generation) return task IDs
- Poll `/api/v1/tasks/:task_id` for status updates

## Running the Application

**Development (recommended):** Run frontend and backend separately for hot reload:
```bash
# Terminal 1: Backend
go run main.go

# Terminal 2: Frontend
cd web && npm run dev
```
- Frontend: http://localhost:3012
- Backend API: http://localhost:5678/api/v1

**Production:** Build frontend, then run backend (serves both):
```bash
cd web && npm run build && cd ..
go run main.go
```
- Access: http://localhost:5678

## FFmpeg Requirement

FFmpeg is required for video processing. Verify installation:
```bash
ffmpeg -version
```
