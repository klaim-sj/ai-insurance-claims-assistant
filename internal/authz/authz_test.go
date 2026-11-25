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
