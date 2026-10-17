# LifeOS AI — Local Setup

Stack: Next.js (frontend) → Go/Gin REST API (backend) → MySQL 8.

## Prerequisites

Git, Go 1.26+, Node.js 20+, MySQL 8 (this machine runs it as the `MySQL80` Windows service on port 3306).

## Database

```sql
CREATE DATABASE IF NOT EXISTS dailytask CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

Apply migrations (run again after every pull; it only applies new files):

```powershell
cd backend
go run ./cmd/migrate
```

Migrations are `backend/migrations/NNNNNN_name.up.sql`, embedded into the binary and tracked in the `schema_migrations` table. Add a new file with the next number; never edit one that has already been applied. `.down.sql` files are kept for manual rollback.

## Backend

```powershell
cd backend
copy .env.example .env   # then fill in DB_PASSWORD
go run ./cmd/api
```

DB settings are separate variables (`DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`) and the DSN is built in code, so passwords with `@`, `#`, `/` need no URL-escaping. Quote the password in `.env` if it contains `#`.

Check: http://localhost:8080/api/v1/health → `{"database":"connected","status":"ok"}`

## Frontend

```powershell
cd frontend
copy .env.example .env.local
npm install
npm run dev
```

Open http://localhost:3000. The home page shows the live API/database status and sign-in state; create an account at `/register`. Restart `npm run dev` after changing `.env.local`.

## Notes

- `.env` and `.env.local` are git-ignored. Never commit them.
- CORS allows exactly `WEB_ORIGIN` (default `http://localhost:3000`).
- Using `root` is acceptable locally only; create a dedicated user before sharing or deploying.
