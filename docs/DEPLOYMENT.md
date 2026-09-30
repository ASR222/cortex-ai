# Deploying CortexAI for free

**The target:** the whole stack on one **Oracle Cloud Always-Free** Arm VM (up to 4 CPUs and 24 GB RAM, free indefinitely). It gets a free **DuckDNS** subdomain, and Caddy provides automatic HTTPS. Every external service used here has a free tier.

| What | Service | Cost |
|---|---|---|
| Server | Oracle Cloud Always Free (Ampere A1) | Free (card needed for identity verification only) |
| Domain | DuckDNS subdomain | Free |
| HTTPS | Let's Encrypt, via Caddy | Free |
| Database, cache, vectors, files | MongoDB, Redis, Qdrant and a disk volume, in Docker on the VM | Free |
| Sign-in | Firebase Authentication (Spark plan) | Free |
| LLMs | Groq (required), Google AI Studio (vision and RAG) | Free tiers |
| Web search | Tavily | Free (1,000 searches a month) |
| Payments | Razorpay **test mode** | Free, no KYC |

Expect about 1–2 hours the first time. Most of it is creating accounts.

---

## 1. Collect your API keys

Put every value into a local `.env` built from `.env.example`. Generate each random secret (`INTERNAL_TOKEN`, `MONGO_PASSWORD`, `REDIS_PASSWORD`, `QDRANT_API_KEY`) with:

```bash
openssl rand -hex 32
```

1. **Firebase** (https://console.firebase.google.com):
   1. Create a project (Google Analytics isn't needed).
   2. Go to **Build → Authentication → Get started → Sign-in method → Google → Enable**.
   3. Go to **Project settings → General → Your apps → Add app → Web**, then copy `apiKey`, `authDomain`, `projectId` and `appId` into `VITE_FIREBASE_API_KEY`, `VITE_FIREBASE_AUTH_DOMAIN`, `FIREBASE_PROJECT_ID` and `VITE_FIREBASE_APP_ID`.
   4. Later, under **Authentication → Settings → Authorized domains**, add your DuckDNS domain.
2. **Groq** (https://console.groq.com/keys): create a key and set it as `GROQ_API_KEY`.
3. **Google AI Studio** (https://aistudio.google.com/apikey): create a key and set it as `GOOGLE_API_KEY`. This enables image understanding and PDF Q&A.
4. **Tavily** (https://app.tavily.com): copy your key into `TAVILY_API_KEY`. This enables web search.
5. **Razorpay** (https://dashboard.razorpay.com):
   1. Sign up and stay in **Test mode** (the toggle at the top).
   2. Go to **Account & Settings → API Keys → Generate test key**, then set `RAZORPAY_KEY_ID` (it starts with `rzp_test_`) and `RAZORPAY_KEY_SECRET`.
   3. Set up the webhook after the site is live (step 5).

> Never reuse keys from someone else's project, and never commit `.env`.

## 2. Create the VM

1. Sign up at https://www.oracle.com/cloud/free/. Choose your **home region** carefully, because Always-Free Arm capacity depends on it.
2. Go to **Compute → Instances → Create instance**:
   - **Image:** Canonical Ubuntu 24.04.
   - **Shape:** Ampere → `VM.Standard.A1.Flex`, 2 OCPU / 12 GB. That's plenty, and within the free allowance.
   - **Networking:** create a VCN with a public subnet and **assign a public IPv4 address**.
   - **SSH keys:** upload your public key, or download the generated one.
   - If you get "Out of capacity", try another availability domain or try again later.
3. Open the web ports in Oracle's firewall. Go to **Networking → Virtual cloud networks → your VCN → Security Lists → Default → Add Ingress Rules**:
   - Source `0.0.0.0/0`, TCP, destination port `80`.
   - Source `0.0.0.0/0`, TCP, destination port `443`.
4. SSH in and open the same ports on the VM itself. Oracle's Ubuntu images block them with iptables by default:

   ```bash
   ssh ubuntu@<public-ip>
   ```

   ```bash
   sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
   ```

   ```bash
   sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
   ```

   ```bash
   sudo netfilter-persistent save
   ```

5. Install Docker:

   ```bash
   curl -fsSL https://get.docker.com | sudo sh
   ```

   ```bash
   sudo usermod -aG docker $USER
   ```

   Then log out and SSH back in so the group change takes effect.

## 3. Point a free domain at it

1. Sign in at https://www.duckdns.org and create a subdomain, for example `cortexai-yourname`.
2. Set its IP to the VM's public IP.
3. Your site will be `https://cortexai-yourname.duckdns.org`.
4. Add that domain to Firebase's **Authorized domains** (step 1.1.4).

## 4. Deploy

```bash
git clone https://github.com/<you>/cortex-ai.git
```

```bash
cd cortex-ai && cp .env.example .env && nano .env
```

Set these for production:

```dotenv
SITE_ADDRESS=cortexai-yourname.duckdns.org
COOKIE_SECURE=true
```

Fill in everything from step 1, then start the stack:

```bash
docker compose up -d --build
```

The first build takes about 5–10 minutes on the Arm VM. Caddy fetches the HTTPS certificate automatically, as long as ports 80 and 443 are reachable. Check on it with:

```bash
docker compose ps
```

```bash
docker compose logs -f web gateway agent
```

Open your domain, sign in with Google and send a message.

## 5. Razorpay webhook

In the Razorpay dashboard, still in test mode, go to **Account & Settings → Webhooks → Add new webhook**:
- **URL:** `https://cortexai-yourname.duckdns.org/api/billing/webhook`
- **Secret:** any random string. Put the same value in `RAZORPAY_WEBHOOK_SECRET` in `.env`.
- **Events:** `payment.captured`, `payment.failed`.

Apply the new secret:

```bash
docker compose up -d billing
```

To test a purchase, open **Credits** in the sidebar, buy a pack and pay with UPI ID `success@razorpay`.

## 6. Operating it

**Update to the latest code:**

```bash
git pull && docker compose up -d --build
```

**Back up MongoDB:**

```bash
docker compose exec -T mongo sh -c 'mongodump -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --archive --gzip' > backup-$(date +%F).gz
```

**Watch errors:**

```bash
docker compose logs -f --since 10m gateway agent
```

**Spending and abuse controls** live in `.env`: `STARTING_CREDITS`, `DAILY_REQUEST_CAP`, and the `RATE_LIMIT_*` settings on the gateway.

### Troubleshooting

| Symptom | Fix |
|---|---|
| Browser can't connect | Check the VCN ingress rules and the VM iptables (step 2.3–2.4). |
| Certificate errors in `docker compose logs web` | DuckDNS must point to the VM, and port 80 must be open for the Let's Encrypt challenge. |
| Sign-in popup error `auth/unauthorized-domain` | Add your domain to Firebase **Authorized domains**. |
| Signed in, but every request returns 401 | `COOKIE_SECURE=true` needs HTTPS; use `false` only for `http://localhost`. |
| "Document Q&A needs a Google API key" | Set `GOOGLE_API_KEY`. |
| Service keeps restarting | Run `docker compose logs <service>`. A startup error lists any missing environment variables by name. |

## Running services natively (development)

Start the databases with `docker-compose.dev.yml` (see the README), then export these variables for each service.

| Service | Variables |
|---|---|
| auth | `MONGODB_URI=mongodb://cortex:<pw>@localhost:27017/?authSource=admin` `INTERNAL_TOKEN` `FIREBASE_PROJECT_ID` |
| chat | `MONGODB_URI` `INTERNAL_TOKEN` `AGENT_SERVICE_URL=http://localhost:8084` |
| billing | `MONGODB_URI` `INTERNAL_TOKEN` `AUTH_SERVICE_URL=http://localhost:8081` `RAZORPAY_KEY_ID` `RAZORPAY_KEY_SECRET` |
| gateway | `REDIS_URL=redis://:<pw>@localhost:6379/0` `INTERNAL_TOKEN` `AUTH_SERVICE_URL` `CHAT_SERVICE_URL` `AGENT_SERVICE_URL` `BILLING_SERVICE_URL` `COOKIE_SECURE=false` |
| agent | `agent/.env` with `INTERNAL_TOKEN` `REDIS_URL` `CHAT_SERVICE_URL=http://localhost:8082` `AUTH_SERVICE_URL=http://localhost:8081` `QDRANT_URL=http://localhost:6333` `QDRANT_API_KEY` `GROQ_API_KEY` `GOOGLE_API_KEY` `TAVILY_API_KEY` `STORAGE_DIR=./data/files` |
| frontend | `frontend/.env` with the `VITE_FIREBASE_*` values |
