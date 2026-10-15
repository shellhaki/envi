// Package secret stores and reads the secrets in an environment: one
// encrypted version per change, a revision number for safe whole-environment
// writes, and an audit event for every read and write.
package secret
