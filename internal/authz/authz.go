// Package authz berisi model role-based access control (RBAC).
//
// Pemeriksaan izin dilakukan di dua lapis: middleware HTTP (route) dan
// service/handler (Require). Keduanya memakai matriks izin yang sama di sini.
package authz

import "errors"

// Role adalah peran pengguna; nilainya sama dengan kolom users.role.
type Role string

const (
	RoleClaimsOfficer Role = "claims_officer"
	RoleSupervisor    Role = "supervisor"
	RoleAdmin         Role = "admin"
)

// Label mengembalikan nama peran untuk ditampilkan di UI.
func (r Role) Label() string {
	switch r {
	case RoleClaimsOfficer:
		return "Claims Officer"
	case RoleSupervisor:
		return "Supervisor"
	case RoleAdmin:
		return "Admin"
	default:
		return "Tidak dikenal"
	}
}

// Valid melaporkan apakah r adalah peran yang dikenal.
func (r Role) Valid() bool {
	switch r {
	case RoleClaimsOfficer, RoleSupervisor, RoleAdmin:
		return true
	}
	return false
}

// Permission adalah izin atas suatu menu/fitur.
type Permission string

const (
	PermDashboardView    Permission = "dashboard.view"
	PermClaimsView       Permission = "claims.view"
	PermClaimsCreate     Permission = "claims.create"
	PermClaimsEdit       Permission = "claims.edit"
	PermClaimsTransition Permission = "claims.transition"
	// PermClaimsViewAll: melihat seluruh klaim tim, bukan hanya yang ditugaskan.
	PermClaimsViewAll Permission = "claims.view_all"
	// PermClaimsReassign: mengubah petugas yang ditugaskan pada klaim.
	PermClaimsReassign Permission = "claims.reassign"
	// PermDocumentsView: pratinjau/unduh dokumen klaim yang boleh diakses.
	PermDocumentsView Permission = "documents.view"
	// PermDocumentsUpload: mengunggah dokumen ke klaim yang boleh diakses.
	PermDocumentsUpload Permission = "documents.upload"
	// PermDocumentsProcess: meminta proses ulang OCR/ekstraksi dokumen.
	PermDocumentsProcess Permission = "documents.process"
	PermReportsView      Permission = "reports.view"
	PermUsersManage      Permission = "users.manage"
)

// ErrForbidden dikembalikan bila peran tidak memiliki izin.
var ErrForbidden = errors.New("akses tidak diizinkan")

// Admin sengaja tidak memiliki izin klaim/dokumen apa pun (tidak mengakses data klaim).
var matrix = map[Role]map[Permission]bool{
	RoleClaimsOfficer: {
		PermDashboardView: true, PermClaimsView: true, PermClaimsCreate: true,
		PermClaimsEdit: true, PermClaimsTransition: true,
		PermDocumentsView: true, PermDocumentsUpload: true, PermDocumentsProcess: true,
	},
	RoleSupervisor: {
		PermDashboardView: true, PermClaimsView: true, PermClaimsCreate: true,
		PermClaimsEdit: true, PermClaimsTransition: true, PermClaimsViewAll: true,
		PermClaimsReassign: true, PermReportsView: true,
		PermDocumentsView: true, PermDocumentsUpload: true, PermDocumentsProcess: true,
	},
	RoleAdmin: {PermDashboardView: true, PermReportsView: true, PermUsersManage: true},
}

// Can melaporkan apakah role memiliki permission. Peran tak dikenal selalu ditolak.
func Can(role Role, perm Permission) bool {
	return matrix[role][perm]
}

// Require mengembalikan ErrForbidden bila role tidak memiliki permission.
// Dipanggil di service/handler sebagai lapis kedua setelah middleware.
func Require(role Role, perm Permission) error {
	if !Can(role, perm) {
		return ErrForbidden
	}
	return nil
}

// CanAccessClaim adalah kebijakan akses per klaim (object-level):
//   - Supervisor: seluruh klaim tim (demo: satu tim, jadi seluruh klaim).
//   - Claims Officer: hanya klaim yang ditugaskan kepadanya.
//   - Admin/peran lain: tidak pernah.
func CanAccessClaim(role Role, userID int64, assignedTo *int64) bool {
	if !Can(role, PermClaimsView) {
		return false
	}
	if Can(role, PermClaimsViewAll) {
		return true
	}
	return assignedTo != nil && *assignedTo == userID
}

// MenuItem adalah satu entri navigasi.
type MenuItem struct {
	Label string
	Path  string
	Perm  Permission
}

var menu = []MenuItem{
	{Label: "Dashboard", Path: "/dashboard", Perm: PermDashboardView},
	{Label: "Klaim", Path: "/claims", Perm: PermClaimsView},
	{Label: "Laporan", Path: "/reports", Perm: PermReportsView},
	{Label: "Pengguna", Path: "/admin/users", Perm: PermUsersManage},
}

// MenuFor mengembalikan menu yang boleh dilihat role, sesuai urutan tetap.
func MenuFor(role Role) []MenuItem {
	var out []MenuItem
	for _, m := range menu {
		if Can(role, m.Perm) {
			out = append(out, m)
		}
	}
	return out
}
