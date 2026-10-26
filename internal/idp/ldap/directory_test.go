package ldap

import (
	"net"
	"strings"
	"sync"
	"testing"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
	"github.com/maxiofs/maxiofs/internal/idp"
)

// directoryEntry is one entry of a fakeDirectory; password is the one a bind
// as its DN takes.
type directoryEntry struct {
	dn       string
	password string
	attrs    map[string][]string
}

// fakeDirectory is an LDAP server for tests: simple binds, and searches with
// a base or subtree scope, the filters the client sends and a size limit
// answered as a directory does (the first entries, then sizeLimitExceeded).
// A frozen one takes connections and never answers.
type fakeDirectory struct {
	entries   []directoryEntry
	entriesMu sync.RWMutex
	frozen    bool

	ln    net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func startDirectory(t *testing.T, d *fakeDirectory) *fakeDirectory {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d.ln = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			d.mu.Lock()
			d.conns = append(d.conns, conn)
			d.mu.Unlock()
			if !d.frozen {
				go d.serve(conn)
			}
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, c := range d.conns {
			c.Close()
		}
	})
	return d
}

func (d *fakeDirectory) port() int { return d.ln.Addr().(*net.TCPAddr).Port }

// snapshot is the entries as they are now.
func (d *fakeDirectory) snapshot() []directoryEntry {
	d.entriesMu.RLock()
	defer d.entriesMu.RUnlock()
	return append([]directoryEntry(nil), d.entries...)
}

// remove takes the entry dn out of the directory.
func (d *fakeDirectory) remove(dn string) {
	d.entriesMu.Lock()
	defer d.entriesMu.Unlock()
	for i, e := range d.entries {
		if strings.EqualFold(e.dn, dn) {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			return
		}
	}
}

// setAttr sets an attribute of the entry dn.
func (d *fakeDirectory) setAttr(dn, name string, values ...string) {
	d.entriesMu.Lock()
	defer d.entriesMu.Unlock()
	for _, e := range d.entries {
		if strings.EqualFold(e.dn, dn) {
			e.attrs[name] = values
		}
	}
}

// config is a provider configuration for this directory, bound as the
// service account.
func (d *fakeDirectory) config() *idp.LDAPConfig {
	return &idp.LDAPConfig{
		Host: "127.0.0.1", Port: d.port(), Security: "none",
		BindDN: "cn=svc,dc=example,dc=org", BindPassword: "svc-pass",
		BaseDN: "dc=example,dc=org", UserSearchBase: "ou=people,dc=example,dc=org",
		UserFilter: "(objectClass=inetOrgPerson)", GroupSearchBase: "ou=groups,dc=example,dc=org",
		GroupFilter: "(objectClass=groupOfNames)", AttrUsername: "uid", AttrEmail: "mail",
		AttrDisplayName: "displayName", AttrMemberOf: "memberOf",
	}
}

// person is an inetOrgPerson under ou=people.
func person(uid, password string) directoryEntry {
	return directoryEntry{
		dn:       "uid=" + uid + ",ou=people,dc=example,dc=org",
		password: password,
		attrs: map[string][]string{
			"objectClass": {"inetOrgPerson"}, "uid": {uid}, "mail": {uid + "@example.org"},
			"displayName": {strings.ToUpper(uid[:1]) + uid[1:]}, "cn": {uid},
		},
	}
}

// group is a groupOfNames under ou=groups with the people members names.
func group(name string, members ...string) directoryEntry {
	dns := make([]string, len(members))
	for i, m := range members {
		dns[i] = "uid=" + m + ",ou=people,dc=example,dc=org"
	}
	return directoryEntry{
		dn:    "cn=" + name + ",ou=groups,dc=example,dc=org",
		attrs: map[string][]string{"objectClass": {"groupOfNames"}, "cn": {name}, "member": dns},
	}
}

// standardDirectory has the service account and joanna, ann and bob, in that
// order: a search for "ann" finds joanna first. ann is in admins, bob and
// joanna in devs; nobody has memberOf.
func standardDirectory() *fakeDirectory {
	return &fakeDirectory{entries: []directoryEntry{
		{dn: "cn=svc,dc=example,dc=org", password: "svc-pass", attrs: map[string][]string{"objectClass": {"organizationalRole"}, "cn": {"svc"}}},
		person("joanna", "joanna-pass"), person("ann", "ann-pass"), person("bob", "bob-pass"),
		group("admins", "ann"), group("devs", "bob", "joanna"),
	}}
}

func (d *fakeDirectory) serve(conn net.Conn) {
	defer conn.Close()
	for {
		packet, err := ber.ReadPacket(conn)
		if err != nil {
			return
		}
		if len(packet.Children) < 2 {
			return
		}
		id := packet.Children[0].Value
		op := packet.Children[1]
		switch op.Tag {
		case ldap.ApplicationBindRequest:
			dn := op.Children[1].Data.String()
			password := op.Children[2].Data.String()
			code := uint16(ldap.LDAPResultInvalidCredentials)
			for _, e := range d.snapshot() {
				if strings.EqualFold(e.dn, dn) && e.password == password {
					code = ldap.LDAPResultSuccess
				}
			}
			d.reply(conn, id, ldap.ApplicationBindResponse, code)
		case ldap.ApplicationSearchRequest:
			d.search(conn, id, op)
		case ldap.ApplicationUnbindRequest:
			return
		default:
			d.reply(conn, id, ldap.ApplicationExtendedResponse, ldap.LDAPResultProtocolError)
		}
	}
}

func (d *fakeDirectory) search(conn net.Conn, id interface{}, op *ber.Packet) {
	base := op.Children[0].Data.String()
	scope := op.Children[1].Value.(int64)
	sizeLimit := op.Children[3].Value.(int64)
	filter := op.Children[6]
	sent := 0
	for _, e := range d.snapshot() {
		inScope := strings.EqualFold(e.dn, base)
		if scope != ldap.ScopeBaseObject {
			inScope = inScope || strings.HasSuffix(strings.ToLower(e.dn), ","+strings.ToLower(base))
		}
		if !inScope || !matches(filter, e) {
			continue
		}
		if sizeLimit > 0 && int64(sent) == sizeLimit {
			d.reply(conn, id, ldap.ApplicationSearchResultDone, ldap.LDAPResultSizeLimitExceeded)
			return
		}
		envelope := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		envelope.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
		result := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ldap.ApplicationSearchResultEntry, nil, "")
		result.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, e.dn, ""))
		attributes := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		for name, values := range e.attrs {
			attribute := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
			attribute.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, ""))
			set := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
			for _, v := range values {
				set.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, v, ""))
			}
			attribute.AppendChild(set)
			attributes.AppendChild(attribute)
		}
		result.AppendChild(attributes)
		envelope.AppendChild(result)
		if _, err := conn.Write(envelope.Bytes()); err != nil {
			return
		}
		sent++
	}
	d.reply(conn, id, ldap.ApplicationSearchResultDone, ldap.LDAPResultSuccess)
}

func (d *fakeDirectory) reply(conn net.Conn, id interface{}, application ber.Tag, code uint16) {
	envelope := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
	envelope.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
	response := ber.Encode(ber.ClassApplication, ber.TypeConstructed, application, nil, "")
	response.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, int64(code), ""))
	response.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	response.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	envelope.AppendChild(response)
	_, _ = conn.Write(envelope.Bytes())
}

// matches evaluates the filters the client sends: and, or, not, equality,
// substrings and presence.
func matches(f *ber.Packet, e directoryEntry) bool {
	values := func(name string) []string {
		for k, v := range e.attrs {
			if strings.EqualFold(k, name) {
				return v
			}
		}
		return nil
	}
	switch f.Tag {
	case ldap.FilterAnd:
		for _, c := range f.Children {
			if !matches(c, e) {
				return false
			}
		}
		return true
	case ldap.FilterOr:
		for _, c := range f.Children {
			if matches(c, e) {
				return true
			}
		}
		return false
	case ldap.FilterNot:
		return !matches(f.Children[0], e)
	case ldap.FilterPresent:
		name := f.Data.String()
		return strings.EqualFold(name, "objectClass") || values(name) != nil
	case ldap.FilterEqualityMatch:
		want := f.Children[1].Data.String()
		for _, v := range values(f.Children[0].Data.String()) {
			if strings.EqualFold(v, want) {
				return true
			}
		}
		return false
	case ldap.FilterSubstrings:
		for _, v := range values(f.Children[0].Data.String()) {
			if substringsMatch(strings.ToLower(v), f.Children[1].Children) {
				return true
			}
		}
		return false
	}
	return false
}

func substringsMatch(v string, parts []*ber.Packet) bool {
	for _, p := range parts {
		s := strings.ToLower(p.Data.String())
		switch p.Tag {
		case ldap.FilterSubstringsInitial:
			if !strings.HasPrefix(v, s) {
				return false
			}
			v = v[len(s):]
		case ldap.FilterSubstringsAny:
			i := strings.Index(v, s)
			if i < 0 {
				return false
			}
			v = v[i+len(s):]
		case ldap.FilterSubstringsFinal:
			if !strings.HasSuffix(v, s) {
				return false
			}
		}
	}
	return true
}
