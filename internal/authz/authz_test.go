package authz

import (
	"errors"
	"testing"
)

func TestMatrix(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleClaimsOfficer, PermDashboardView, true},
		{RoleClaimsOfficer, PermClaimsView, true},
		{RoleClaimsOfficer, PermReportsView, false},
		{RoleClaimsOfficer, PermUsersManage, false},
		{RoleSupervisor, PermClaimsView, true},
		{RoleSupervisor, PermReportsView, true},
		{RoleSupervisor, PermUsersManage, false},
		{RoleAdmin, PermReportsView, true},
		{RoleAdmin, PermUsersManage, true},
		{RoleAdmin, PermClaimsView, false},
		{Role("hacker"), PermDashboardView, false},
		{Role(""), PermDashboardView, false},
	}
	for _, c := range cases {
		if got := Can(c.role, c.perm); got != c.want {
			t.Errorf("Can(%q, %q) = %v, want %v", c.role, c.perm, got, c.want)
		}
		err := Require(c.role, c.perm)
		if c.want && err != nil {
			t.Errorf("Require(%q, %q) error tak terduga: %v", c.role, c.perm, err)
		}
		if !c.want && !errors.Is(err, ErrForbidden) {
			t.Errorf("Require(%q, %q) harus ErrForbidden, dapat %v", c.role, c.perm, err)
		}
	}
}

func TestMenuFor(t *testing.T) {
	paths := func(r Role) []string {
		var p []string
		for _, m := range MenuFor(r) {
			p = append(p, m.Path)
		}
		return p
	}
	want := map[Role][]string{
		RoleClaimsOfficer: {"/dashboard", "/claims"},
		RoleSupervisor:    {"/dashboard", "/claims", "/reports"},
		RoleAdmin:         {"/dashboard", "/reports", "/admin/users"},
	}
	for role, w := range want {
		got := paths(role)
		if len(got) != len(w) {
			t.Fatalf("menu %s = %v, want %v", role, got, w)
		}
		for i := range w {
			if got[i] != w[i] {
				t.Fatalf("menu %s = %v, want %v", role, got, w)
			}
		}
	}
	if len(MenuFor("unknown")) != 0 {
		t.Fatal("peran tak dikenal tidak boleh punya menu")
	}
}

func TestClaimPermissions(t *testing.T) {
	claimPerms := []Permission{PermClaimsView, PermClaimsCreate, PermClaimsEdit,
		PermClaimsTransition, PermClaimsViewAll, PermClaimsReassign}
	for _, p := range claimPerms {
		if Can(RoleAdmin, p) {
			t.Errorf("admin tidak boleh punya izin klaim %q", p)
		}
		if !Can(RoleSupervisor, p) {
			t.Errorf("supervisor harus punya izin %q", p)
		}
	}
	if Can(RoleClaimsOfficer, PermClaimsViewAll) || Can(RoleClaimsOfficer, PermClaimsReassign) {
		t.Fatal("officer tidak boleh melihat semua klaim atau reassign")
	}
}

func TestCanAccessClaim(t *testing.T) {
	own, other := int64(10), int64(20)
	cases := []struct {
		name     string
		role     Role
		assigned *int64
		want     bool
	}{
		{"officer klaim sendiri", RoleClaimsOfficer, &own, true},
		{"officer klaim orang lain", RoleClaimsOfficer, &other, false},
		{"officer klaim tanpa petugas", RoleClaimsOfficer, nil, false},
		{"supervisor klaim mana pun", RoleSupervisor, &other, true},
		{"supervisor klaim tanpa petugas", RoleSupervisor, nil, true},
		{"admin klaim sendiri pun tidak", RoleAdmin, &own, false},
		{"peran tak dikenal", Role("x"), &own, false},
	}
	for _, c := range cases {
		if got := CanAccessClaim(c.role, own, c.assigned); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
