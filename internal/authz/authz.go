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
	PermDashboardView Permission = "dashboard.view"
	PermClaimsView    Permission = "claims.view"
	PermReportsView   Permission = "reports.view"
	PermUsersManage   Permission = "users.manage"
)

// ErrForbidden dikembalikan bila peran tidak memiliki izin.
var ErrForbidden = errors.New("akses tidak diizinkan")

var matrix = map[Role]map[Permission]bool{
	RoleClaimsOfficer: {PermDashboardView: true, PermClaimsView: true},
	RoleSupervisor:    {PermDashboardView: true, PermClaimsView: true, PermReportsView: true},
	RoleAdmin:         {PermDashboardView: true, PermReportsView: true, PermUsersManage: true},
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
