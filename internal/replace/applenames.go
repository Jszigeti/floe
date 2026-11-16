package replace

// appleNames are the folders and files Apple's own apps and daemons keep in
// ~/Library/Application Support without the com.apple. prefix, measured on the
// Pro on 2026-11-16. A trailing * matches any suffix. A fresh account creates
// some of them at login (Present covers those); this list covers the rest.
var appleNames = []string{
	"AddressBook",             // Contacts database
	"Animoji",                 // Messages stickers
	"AskPermission",           // Screen Time purchase requests
	"appplaceholdersyncd",     // App Store placeholder daemon
	"assistant_service",       // Siri
	"CallHistoryDB",           // Phone and FaceTime calls
	"CallHistoryTransactions", // Phone and FaceTime calls
	"CloudDocs",               // iCloud Drive state
	"CloudKit*",               // iCloud database, its -shm and -wal, its assets
	"contactsd",               // Contacts daemon
	"ControlCenter",           // Control Center
	"CrashReporter",           // crash reporter state
	"default.store*",          // Core Data store of Apple services, its -shm and -wal
	"DifferentialPrivacy",     // analytics daemon
	"FaceTime",                // FaceTime
	"familycircled",           // Family Sharing
	"FileProvider",            // iCloud Drive and cloud storage state
	"homeenergyd",             // Home energy daemon
	"icdd",                    // Image Capture daemon
	"iCloud",                  // iCloud account state
	"icloudmailagent",         // iCloud Mail daemon
	"identityservicesd",       // iMessage and FaceTime identity
	"IntelligenceFlow",        // Apple Intelligence
	"Knowledge",               // Screen Time and usage database
	"Local.store*",            // Core Data store of Apple services, its -shm and -wal
	"locationaccessstored",    // location permissions
	"MobileSync",              // iPhone and iPad sync and backups
	"Music",                   // Music app playback sessions
	"networkserviceproxy",     // iCloud Private Relay
	"SESStorage",              // Secure Element (Apple Pay)
	"Spotlight",               // Spotlight
	"SyncServices",            // sync daemon
	"tipsd",                   // Tips
}
