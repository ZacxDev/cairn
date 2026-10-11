package redact

import (
	"bytes"
	"testing"
)

// A synthetic, code-shaped CLEAN corpus written for this audit: nothing in it is a secret, so
// every changed byte is damage.
// TestAuditCodeShapedCleanCorpusIsUndamaged: 40 of 75 lines damaged before any code filter, 4 of
// 75 at 2ba3e5c (`!vault`, `********`, `your_password_here`, `your-api-key`); asserted 0 now.
func TestAuditCodeShapedCleanCorpusIsUndamaged(t *testing.T) {
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	lines := []string{
		// Go
		`	token := r.Header.Get("Authorization")`,
		`	password := os.Getenv("DB_PASSWORD")`,
		`	cfg.Password = password`,
		`	Password: cfg.Password,`,
		`	apiKey: opts.APIKey,`,
		`	secret, err := loadSecret(ctx, name)`,
		`	if token == "" {`,
		`	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok})`,
		`	AccessToken: tok,`,
		`	ClientSecret: os.Getenv("CLIENT_SECRET"),`,
		`	const maxTokens = 4096`,
		`	SessionToken: creds.SessionToken,`,
		// Python
		`api_key = os.environ["API_KEY"]`,
		`api_key = os.getenv("API_KEY", "")`,
		`password = getpass.getpass()`,
		`password = args.password`,
		`token = response.json()["access_token"]`,
		`secret_key = settings.SECRET_KEY`,
		`SECRET_KEY = env("DJANGO_SECRET_KEY")`,
		`    password: str = Field(..., min_length=8)`,
		`    token: Optional[str] = None`,
		`    api_key: str`,
		`self.token = token`,
		`DB_PASSWORD = config('DB_PASSWORD')`,
		// JS/TS
		`const token = await getToken();`,
		`const apiKey = process.env.API_KEY;`,
		`  password: process.env.DB_PASSWORD,`,
		`  token: req.headers.authorization,`,
		`  secret: config.jwtSecret,`,
		`export const API_KEY = import.meta.env.VITE_API_KEY;`,
		`  accessToken: string;`,
		`  password?: string;`,
		`let refreshToken = localStorage.getItem('refresh_token');`,
		// YAML / CI / k8s
		`  password: ${{ secrets.DB_PASSWORD }}`,
		`  token: ${{ github.token }}`,
		`      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`,
		`  password: ${DB_PASSWORD}`,
		`  password: !vault |`,
		`    secretName: db-credentials`,
		`  auth: true`,
		`  token: ~`,
		`  password: "{{ vault_db_password }}"`,
		`  client_secret: "{{ .Values.oidc.clientSecret }}"`,
		`  api_key: <your-api-key-here>`,
		`  password: <REDACTED>`,
		`  token: "<TOKEN>"`,
		`  password: xxxxxxxx`,
		`  password: ********`,
		// HCL
		`  password = var.db_password`,
		`  password = random_password.db.result`,
		`  token    = data.vault_generic_secret.gh.data["token"]`,
		`  api_key  = local.api_key`,
		// shell / dotenv
		`export DB_PASSWORD="$(pass show db/prod)"`,
		`export GITHUB_TOKEN=$(gh auth token)`,
		`export API_KEY=${API_KEY:-}`,
		`DB_PASSWORD_FILE=/run/secrets/db_password`,
		`TOKEN_URL=https://auth.example.test/oauth/token`,
		`read -s PASSWORD`,
		`curl -u "$USER:$PASS" https://x.example.test`,
		`docker run -e DB_PASSWORD=$DB_PASSWORD app`,
		`docker run -e API_TOKEN -e LOG=1 app`,
		`password=your_password_here`,
		`API_KEY=your-api-key`,
		`SECRET=...`,
		// Markdown / prose
		`Set ` + "`DB_PASSWORD`" + ` to the database password before running.`,
		`- **token**: the bearer token issued by the IdP (see below).`,
		`Authorization: Bearer <token>`,
		`Authorization: Bearer $TOKEN`,
		`| password | string | the user's password |`,
		`The secret: keep it out of the logs.`,
		// Rust / Java
		`    let token = std::env::var("TOKEN")?;`,
		`    String password = System.getenv("DB_PASSWORD");`,
		`    private String apiKey;`,
		`    @Value("${app.jwt.secret}")`,
		`    pub api_key: String,`,
	}
	damaged := 0
	for _, l := range lines {
		in := l + "\n"
		if out, _ := r.String(in); out != in {
			damaged++
			t.Errorf("damaged: %q -> %q", l, out)
		}
	}
	t.Logf("damaged=%d/%d", damaged, len(lines))
}

// TestSecretKeyRoundThreeNames is review round 2's R3 table: the END rule lost glued, suffixed and
// `<VENDOR>_KEY` names, and must still refuse the non-secrets beside them.
func TestSecretKeyRoundThreeNames(t *testing.T) {
	yes := []string{"PGPASSWORD", "MYSQL_PWD", "SMTP_PASS", "SECRET_KEY_BASE", "apiKeyValue", "clientSecretValue",
		"APP_KEY", "ENCRYPTION_KEY", "JWT_SIGNING_KEY", "OPENAI_KEY", "STRIPE_KEY", "pass", "pwd", "MYSECRET",
		"AccountKey", "SharedAccessKey", "Password"}
	no := []string{"max_tokens", "TOKEN_URL", "passwordHash", "secretKeyRef", "PWD", "OLDPWD", "BYPASS", "COMPASS",
		"PRIMARY_KEY", "SORT_KEY", "CACHE_KEY", "oauth", "DB_PASSWORD_FILE", "apiKeySource", "password_hash"}
	for _, k := range yes {
		if !SecretKey(k) {
			t.Errorf("SecretKey(%q) = false, want true", k)
		}
	}
	for _, k := range no {
		if SecretKey(k) {
			t.Errorf("SecretKey(%q) = true, want false", k)
		}
	}
}
