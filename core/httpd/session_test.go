// This file is part of Monsti, a web content management system.
// Copyright 2012-2013 Christian Neumann
//
// Monsti is free software: you can redistribute it and/or modify it under the
// terms of the GNU Affero General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option) any
// later version.
//
// Monsti is distributed in the hope that it will be useful, but WITHOUT ANY
// WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR
// A PARTICULAR PURPOSE.  See the GNU Affero General Public License for more
// details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Monsti.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"

	"code.google.com/p/go.crypto/bcrypt"
	"pkg.monsti.org/monsti/api/service"
)

func TestCheckPermission(t *testing.T) {
	tests := []struct {
		Action      service.Action
		Auth, Grant bool
	}{
		{service.LoginAction, false, true},
		{service.LoginAction, true, true},
		{service.LogoutAction, false, false},
		{service.LogoutAction, true, true},
		{service.EditAction, false, false},
		{service.EditAction, true, true},
		{service.AddAction, false, false},
		{service.AddAction, true, true},
		{service.RemoveAction, false, false},
		{service.RemoveAction, true, true}}
	for _, v := range tests {
		var user *service.User
		if v.Auth {
			user = &service.User{}
		}
		ret := checkPermission(v.Action, &service.UserSession{User: user})
		if ret != v.Grant {
			t.Errorf("checkPermission(%v, %v) = %v, expected %v", v.Action,
				user, ret, v.Grant)
		}
	}
}

func TestGetUser(t *testing.T) {
	root, err := ioutil.TempDir("", "_monsti_get_user")
	if err != nil {
		t.Fatalf("Could not create temp dir: %s", err)
	}
	db := []byte(`- login: foo
  name: Mr. Foo
  password: the pass
  email: foo@example.com
- login: bar
  name: Mrs. Bar
  email: bar@example.com
  password: other pass
`)
	if err = ioutil.WriteFile(filepath.Join(root, "users.yaml"),
		db, 0600); err != nil {
		t.Fatalf("Could not write navigation: %s", err)
	}
	tests := []struct {
		Login string
		User  *service.User
	}{
		{Login: "unknown", User: nil},
		{Login: "foo", User: &service.User{Login: "foo", Password: "the pass",
			Name: "Mr. Foo", Email: "foo@example.com"}},
		{Login: "bar", User: &service.User{Login: "bar", Password: "other pass",
			Name: "Mrs. Bar", Email: "bar@example.com"}}}
	for _, v := range tests {
		user, err := getUser(v.Login, root)
		if !reflect.DeepEqual(user, v.User) || err != nil {
			t.Errorf("getUser(%q, _) = %v,%v should be %v, nil", v.Login, err,
				user, v.User)
		}
	}
}

func TestPasswordEqual(t *testing.T) {
	tests := []struct {
		ToHash, Password string
		Equal            bool
	}{
		{"foobar", "foobar", true},
		{"foobar", "foo", false},
		{"foobar", "", false}}
	for _, v := range tests {
		hash, err := bcrypt.GenerateFromPassword([]byte(v.ToHash), 0)
		if err != nil {
			t.Fatalf("Could not generate password hash: %v", err)
		}
		if passwordEqual(string(hash), v.Password) != v.Equal {
			t.Errorf("passwordEqual(%v, %v) = %v, should be %v",
				hash, v.Password, !v.Equal, v.Equal)
		}
	}
}