// Package migrations menyematkan file migration goose agar bisa dijalankan
// dari kode (misalnya oleh test) tanpa bergantung pada working directory.
package migrations

import "embed"

// FS berisi seluruh file migration SQL.
//
//go:embed *.sql
var FS embed.FS
