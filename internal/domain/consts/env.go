package consts

// Environment variables, each matching its flag name where there is one.
const (
	EnvWebAddress  = "TUBARR_WEB_ADDRESS"  // --web-address.
	EnvWebAuth     = "TUBARR_WEB_AUTH"     // --web-auth.
	EnvWebHTTPS    = "TUBARR_WEB_HTTPS"    // --web-https.
	EnvWebTLSCert  = "TUBARR_WEB_TLS_CERT" // --web-tls-cert.
	EnvWebTLSKey   = "TUBARR_WEB_TLS_KEY"  // --web-tls-key.
	EnvWebUsername = "TUBARR_WEB_USERNAME" // Sets the login at startup, e.g. for a forgotten password.
	EnvWebPassword = "TUBARR_WEB_PASSWORD" // Sets the login at startup, e.g. for a forgotten password.
	EnvLogToken    = "TUBARR_LOG_TOKEN"    // Passes the log token to Metarr.
	EnvLogURL      = "TUBARR_LOG_URL"      // Passes the URL Metarr sends its logs to.

	EnvWebAllowedHosts = "TUBARR_WEB_ALLOWED_HOSTS" // Comma-separated hostnames allowed alongside those saved in Settings.
)

// Viper supported extensions.
var ViperValidConfigExts = map[string]struct{}{
	".yaml": {},
	".yml":  {},
	".toml": {},
	".json": {},
}
