package consts

// Recommended permissions for different types of files and directories Tubarr might create.
const (
	// ** World Readable **
	// General directories
	PermsGenericDir  = 0o755
	PermsHomeProgDir = 0o700

	// Files
	PermsLogFile = 0o644

	// ** Private **
	// Sensitive files
	PermsPrivateFile = 0o600 // May contain sensitive data
)
