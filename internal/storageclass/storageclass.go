// Package storageclass names the S3 storage classes: their order from the
// warmest to the coldest, and those a lifecycle rule or a cluster can use.
package storageclass

import "slices"

const (
	Standard           = "STANDARD"
	ReducedRedundancy  = "REDUCED_REDUNDANCY"
	StandardIA         = "STANDARD_IA"
	IntelligentTiering = "INTELLIGENT_TIERING"
	OnezoneIA          = "ONEZONE_IA"
	GlacierIR          = "GLACIER_IR"
	Glacier            = "GLACIER"
	DeepArchive        = "DEEP_ARCHIVE"
)

// coldness orders the classes a lifecycle rule moves objects to, as S3 does.
// Any other class is as warm as STANDARD.
var coldness = map[string]int{
	StandardIA:         1,
	IntelligentTiering: 2,
	OnezoneIA:          3,
	GlacierIR:          4,
	Glacier:            5,
	DeepArchive:        6,
}

// Colder reports whether class to is colder than class from: a lifecycle rule
// moves an object to a colder class only.
func Colder(to, from string) bool {
	return coldness[to] > coldness[from]
}

// Known reports whether class is one an object can be written in.
func Known(class string) bool {
	return class == Standard || class == ReducedRedundancy || Transitional(class)
}

// Transitional reports whether a lifecycle rule can move objects to class.
func Transitional(class string) bool {
	_, ok := coldness[class]
	return ok
}

// Definable are the classes a cluster can keep on nodes of their own, from
// the warmest; STANDARD is every other node.
var Definable = []string{ReducedRedundancy, StandardIA, IntelligentTiering, OnezoneIA, GlacierIR, Glacier, DeepArchive}

// IsDefinable reports whether a cluster can keep class on nodes of its own.
func IsDefinable(class string) bool {
	return slices.Contains(Definable, class)
}
