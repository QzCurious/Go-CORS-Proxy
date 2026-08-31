// Package managedpac classifies current-user PAC settings, owns PAC URL
// generation and serial retrying delivery, and completely tears down
// active marker-owned state. It drives platform PAC mechanics through
// pacsettings without exposing product ownership policy to that library.
package managedpac
