# 🚀 Serv00 Log Ingestion & Bug Tracker API Service

Layanan REST API ringan dan hemat resource yang berjalan di **Serv00** (FreeBSD / Linux shared hosting). Layanan ini bertanggung jawab untuk menerima log error sistem, melakukan sanitasi data sensitif, mengagregasi error (deduplikasi), menyimpan riwayat, dan mengelola alur kerja persetujuan Owner (**Owner Approval Workflow**).

---

## 🏗️ Arsitektur & Prinsip

1. **Ultra-Lightweight**: Didesain khusus untuk limit memori Serv00. Menggunakan SQLite embedded (`better-sqlite3` dalam mode WAL) yang cepat dengan konsumsi RAM < 30MB.
2. **Log Sanitizer**: Otomatis menyamarkan password, API token, Bearer auth, private key, dan secret credential sebelum disimpan.
3. **Deterministic Fingerprinting**: Mengelompokkan insiden error yang sama berdasarkan hash normalisasi (mengabaikan dynamic ID/timestamp) dan meningkatkan counter `occurrences`.
4. **Owner Control**: Mengunci aksi modifikasi kode — AGY di Sandbox **hanya boleh** membuat branch dan menguji perbaikan setelah Owner memberikan `APPROVED`.

---

## 📂 Struktur Direktori

```text
serv00/
├── src/
│   ├── api/
│   │   ├── routes/
│   │   │   ├── health.ts    # GET /api/health
│   │   │   ├── logs.ts      # POST /api/logs, GET /api/logs, GET /api/logs/:id
│   │   │   └── bugs.ts      # GET /api/bugs, POST /api/bugs, /approve, /reject, /status
│   │   └── server.ts        # Express App, Helmet, Rate Limiter, CORS
│   ├── auth/
│   │   ├── middleware.ts    # requireApiKey, requireOwnerToken
│   │   └── tokens.ts        # Constant-time token comparator
│   ├── bugs/
│   │   ├── manager.ts       # Bug lifecycle & decision management
│   │   └── types.ts         # Bug schemas & workflow states
│   ├── logs/
│   │   ├── fingerprint.ts   # SHA-256 error normalizer & hasher
│   │   ├── sanitizer.ts     # Regex credential & secret masking
│   │   └── types.ts         # Log schemas
│   ├── storage/
│   │   └── db.ts            # SQLite database manager & retention cleaner
│   ├── config.ts            # Env configuration
│   └── index.ts             # Service entry point
├── tests/                   # Vitest unit & integration test suite
├── .env.example
├── package.json
├── tsconfig.json
└── README.md
```

---

## ⚙️ Konfigurasi Environment (`.env`)

Salin file `.env.example` ke `.env`:

```bash
PORT=7095
NODE_ENV=production
DB_PATH=./data/serv00_bug_tracker.db
API_KEY=serv00_sandbox_secure_token_change_in_production
OWNER_TOKEN=owner_secret_approval_token_change_in_production
LOG_RETENTION_DAYS=14
RATE_LIMIT_PER_MINUTE=120
```

---

## 🚀 Panduan Instalasi & Menjalankan di Serv00

### 1. Build & Install Dependencies
```bash
cd serv00
npm install
npm run build
```

### 2. Jalankan Service
```bash
# Mode Production
npm start

# Atau menggunakan PM2 / Supervisor di Serv00
pm2 start dist/index.js --name "serv00-bug-tracker"
```

---

## 📡 API Reference

### 1. Health Check
- **Endpoint**: `GET /api/health`
- **Auth**: Tidak perlu.

### 2. Log Ingestion
- **Endpoint**: `POST /api/logs`
- **Header**: `Authorization: Bearer <API_KEY>`
- **Payload**:
```json
{
  "level": "error",
  "service": "botgo",
  "message": "panic: runtime error in command .sticker",
  "stack": "cmd/sticker.go:65",
  "timestamp": "2026-08-23T10:18:00Z",
  "metadata": {
    "sender": "628123456789@s.whatsapp.net"
  }
}
```

### 3. Query Unprocessed Logs (Untuk Sandbox Collector)
- **Endpoint**: `GET /api/logs?status=unprocessed&limit=20`
- **Header**: `Authorization: Bearer <API_KEY>`

### 4. Query Bugs
- **Endpoint**: `GET /api/bugs?status=WAITING_FOR_OWNER_APPROVAL`
- **Header**: `Authorization: Bearer <API_KEY>`

### 5. Owner Approval (MANDATORI)
- **Endpoint**: `POST /api/bugs/:id/approve`
- **Header**: `Authorization: Bearer <OWNER_TOKEN>`
- **Payload**:
```json
{
  "approved": true,
  "note": "Approved, silakan perbaiki dan jalankan test."
}
```

### 6. Owner Rejection
- **Endpoint**: `POST /api/bugs/:id/reject`
- **Header**: `Authorization: Bearer <OWNER_TOKEN>`
- **Payload**:
```json
{
  "reason": "Jangan ubah bagian database auth."
}
```

---

## 🧪 Menjalankan Pengujian
```bash
npm test
```
Semua 13 unit & API tests akan divalidasi.
