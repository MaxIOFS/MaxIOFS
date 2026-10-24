package acl

import "encoding/xml"

// S3AccessControlPolicy represents the S3 XML format for ACLs
type S3AccessControlPolicy struct {
	XMLName           xml.Name            `xml:"AccessControlPolicy"`
	Owner             S3Owner             `xml:"Owner"`
	AccessControlList S3AccessControlList `xml:"AccessControlList"`
}

// S3Owner represents the owner in S3 XML format
type S3Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName,omitempty"`
}

// S3AccessControlList represents the grant list in S3 XML format
type S3AccessControlList struct {
	Grants []S3Grant `xml:"Grant"`
}

// S3Grant represents a single grant in S3 XML format
type S3Grant struct {
	Grantee    S3Grantee `xml:"Grantee"`
	Permission string    `xml:"Permission"`
}

// S3Grantee represents a grantee in S3 XML format
type S3Grantee struct {
	XMLName      xml.Name `xml:"Grantee"`
	Type         string   `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	ID           string   `xml:"ID,omitempty"`
	DisplayName  string   `xml:"DisplayName,omitempty"`
	EmailAddress string   `xml:"EmailAddress,omitempty"`
	URI          string   `xml:"URI,omitempty"`
}

const (
	// S3Namespace is the namespace of the S3 API's documents.
	S3Namespace = "http://s3.amazonaws.com/doc/2006-03-01/"
	// xsiNamespace is the namespace of the xsi:type a grantee is typed with.
	xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"
)

// MarshalXML writes the policy as AWS writes it, in the S3 namespace. Reading
// one accepts it with or without.
func (p S3AccessControlPolicy) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	type plain S3AccessControlPolicy
	return e.EncodeElement(plain(p), InS3Namespace("AccessControlPolicy"))
}

// InS3Namespace is the start of an element named local that declares the S3
// namespace, as AWS writes the root of a document.
func InS3Namespace(local string) xml.StartElement {
	return xml.StartElement{
		Name: xml.Name{Local: local},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: S3Namespace}},
	}
}

// MarshalXML writes the grantee as AWS writes it: its type in xsi:type, with
// the xsi prefix declared on the grantee. SDKs read the attribute by that
// name, prefix included.
func (g S3Grantee) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start = xml.StartElement{Name: xml.Name{Local: "Grantee"}}
	if g.Type != "" {
		start.Attr = []xml.Attr{
			{Name: xml.Name{Local: "xmlns:xsi"}, Value: xsiNamespace},
			{Name: xml.Name{Local: "xsi:type"}, Value: g.Type},
		}
	}
	return e.EncodeElement(struct {
		ID           string `xml:"ID,omitempty"`
		DisplayName  string `xml:"DisplayName,omitempty"`
		EmailAddress string `xml:"EmailAddress,omitempty"`
		URI          string `xml:"URI,omitempty"`
	}{g.ID, g.DisplayName, g.EmailAddress, g.URI}, start)
}

// ToS3Format converts internal ACL to S3 XML format
func (a *ACL) ToS3Format() *S3AccessControlPolicy {
	s3ACL := &S3AccessControlPolicy{
		Owner: S3Owner{
			ID:          a.Owner.ID,
			DisplayName: a.Owner.DisplayName,
		},
		AccessControlList: S3AccessControlList{
			Grants: make([]S3Grant, len(a.Grants)),
		},
	}

	for i, grant := range a.Grants {
		s3ACL.AccessControlList.Grants[i] = S3Grant{
			Grantee: S3Grantee{
				Type:         string(grant.Grantee.Type),
				ID:           grant.Grantee.ID,
				DisplayName:  grant.Grantee.DisplayName,
				EmailAddress: grant.Grantee.EmailAddress,
				URI:          grant.Grantee.URI,
			},
			Permission: string(grant.Permission),
		}
	}

	return s3ACL
}

// FromS3Format converts S3 XML format to internal ACL
func FromS3Format(s3ACL *S3AccessControlPolicy) *ACL {
	acl := &ACL{
		Owner: Owner{
			ID:          s3ACL.Owner.ID,
			DisplayName: s3ACL.Owner.DisplayName,
		},
		Grants: make([]Grant, len(s3ACL.AccessControlList.Grants)),
	}

	for i, s3Grant := range s3ACL.AccessControlList.Grants {
		acl.Grants[i] = Grant{
			Grantee: Grantee{
				Type:         GranteeType(s3Grant.Grantee.Type),
				ID:           s3Grant.Grantee.ID,
				DisplayName:  s3Grant.Grantee.DisplayName,
				EmailAddress: s3Grant.Grantee.EmailAddress,
				URI:          s3Grant.Grantee.URI,
			},
			Permission: Permission(s3Grant.Permission),
		}
	}

	return acl
}
