// Copyright 2019 Anapaya Systems
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package snet

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/segment/iface"
	"github.com/scionproto/scion/pkg/slayers"
)

const (
	// LatencyUnset is the default value for a Latency entry in PathMetadata for
	// which no latency was announced.
	LatencyUnset time.Duration = -1
)

// DataplanePath is an abstract representation of a SCION dataplane path.
type DataplanePath interface {
	// SetPath sets the path in the SCION header. It assumes that all the fields
	// except the path and path type are set correctly.
	SetPath(scion *slayers.SCION) error
}

// Path is an abstract representation of a path. Most applications do not need
// access to the raw internals.
//
// An empty path is a special kind of path that can be used for intra-AS
// traffic. Empty paths are valid return values for certain route calls (e.g.,
// if the source and destination ASes match, or if a router was configured
// without a source of paths). An empty path only contains a Destination value,
// all other values are zero values.
type Path interface {
	// UnderlayNextHop returns the address:port pair of a local-AS underlay
	// speaker. Usually, this is a border router that will forward the traffic.
	UnderlayNextHop() *net.UDPAddr
	// Dataplane returns a path that should be used in the dataplane. The
	// underlying dataplane path object is returned directly without any copy.
	// If you modify the raw data, you must ensure that there are no data races
	// or data corruption on your own.
	Dataplane() DataplanePath
	// Source is the AS the path starts from. Empty paths return the local
	// AS of the router that created them.
	Source() addr.IA
	// Destination is the AS the path points to. Empty paths return the local
	// AS of the router that created them.
	Destination() addr.IA
	// Metadata returns supplementary information about this path.
	// Returns nil if the metadata is not available.
	Metadata() *PathMetadata
}

// PathInterface is an interface of the path.
type PathInterface struct {
	// ID is the ID of the interface.
	ID iface.ID
	// IA is the ISD AS identifier of the interface.
	IA addr.IA
}

func (iface PathInterface) String() string {
	return fmt.Sprintf("%s#%d", iface.IA, iface.ID)
}

// EpicAuths is a container for the EPIC hop authenticators.
type EpicAuths struct {
	// AuthPHVF is the authenticator for the penultimate hop.
	AuthPHVF []byte
	// AuthLHVF is the authenticator for the last hop
	AuthLHVF []byte
}

func (ea *EpicAuths) SupportsEpic() bool {
	return (len(ea.AuthPHVF) == 16 && len(ea.AuthLHVF) == 16)
}

// PathMetadata contains supplementary information about a path.
//
// The information about MTU, Latency, Bandwidth etc. are based solely on data
// contained in the AS entries in the path construction beacons. These entries
// are signed/verified based on the control plane PKI. However, the
// *correctness* of this meta data has *not* been checked.
type PathMetadata struct {
	// Interfaces is a list of interfaces on the path.
	Interfaces []PathInterface

	// MTU is the maximum transmission unit for the path, in bytes.
	MTU uint16

	// Expiry is the expiration time of the path.
	Expiry time.Time

	// Latency lists the latencies between any two consecutive interfaces.
	// Entry i describes the latency between interface i and i+1.
	// Consequently, there are N-1 entries for N interfaces.
	// A negative value (LatencyUnset) indicates that the AS did not announce a
	// latency for this hop.
	Latency []time.Duration

	// Bandwidth lists the bandwidth between any two consecutive interfaces, in Kbit/s.
	// Entry i describes the bandwidth between interfaces i and i+1.
	// A 0-value indicates that the AS did not announce a bandwidth for this hop.
	Bandwidth []uint64

	// Geo lists the geographical position of the border routers along the path.
	// Entry i describes the position of the router for interface i.
	// A 0-value indicates that the AS did not announce a position for this router.
	Geo []GeoCoordinates

	// LinkType contains the announced link type of inter-domain links.
	// Entry i describes the link between interfaces 2*i and 2*i+1.
	LinkType []LinkType

	// InternalHops lists the number of AS internal hops for the ASes on path.
	// Entry i describes the hop between interfaces 2*i+1 and 2*i+2 in the same AS.
	// Consequently, there are no entries for the first and last ASes, as these
	// are not traversed completely by the path.
	InternalHops []uint32

	// Notes contains the notes added by ASes on the path, in the order of occurrence.
	// Entry i is the note of AS i on the path.
	Notes []string

	// EpicAuths contains the EPIC authenticators.
	EpicAuths EpicAuths
}

func (pm *PathMetadata) Copy() *PathMetadata {
	if pm == nil {
		return nil
	}

	return &PathMetadata{
		Interfaces:   append(pm.Interfaces[:0:0], pm.Interfaces...),
		MTU:          pm.MTU,
		Expiry:       pm.Expiry,
		Latency:      append(pm.Latency[:0:0], pm.Latency...),
		Bandwidth:    append(pm.Bandwidth[:0:0], pm.Bandwidth...),
		Geo:          append(pm.Geo[:0:0], pm.Geo...),
		LinkType:     append(pm.LinkType[:0:0], pm.LinkType...),
		InternalHops: append(pm.InternalHops[:0:0], pm.InternalHops...),
		Notes:        append(pm.Notes[:0:0], pm.Notes...),
		EpicAuths: EpicAuths{
			AuthPHVF: append([]byte(nil), pm.EpicAuths.AuthPHVF...),
			AuthLHVF: append([]byte(nil), pm.EpicAuths.AuthLHVF...),
		},
	}
}

// Fingerprint is a convenience method that returns the path fingerprint. If the
// receiver is nil, it returns the path fingerprint of the empty interface list.
func (pm *PathMetadata) Fingerprint() PathFingerprint {
	if pm == nil {
		return Fingerprint(nil)
	}
	return Fingerprint(pm.Interfaces)
}

// LinkType describes the underlying network for inter-domain links.
type LinkType uint8

// LinkType values
const (
	// LinkTypeUnset represents an unspecified link type.
	LinkTypeUnset LinkType = iota
	// LinkTypeDirect represents a direct physical connection.
	LinkTypeDirect
	// LinkTypeMultihop represents a connection with local routing/switching.
	LinkTypeMultihop
	// LinkTypeOpennet represents a connection overlayed over publicly routed Internet.
	LinkTypeOpennet
)

func (lt LinkType) String() string {
	switch lt {
	case LinkTypeDirect:
		return "direct"
	case LinkTypeMultihop:
		return "multihop"
	case LinkTypeOpennet:
		return "opennet"
	default:
		return "unset"
	}
}

// GeoCoordinates describes a geographical position (of a border router on the path).
type GeoCoordinates struct {
	// Latitude of the geographic coordinate, in the WGS 84 datum.
	Latitude float32
	// Longitude of the geographic coordinate, in the WGS 84 datum.
	Longitude float32
	// Civic address of the location.
	Address string
}

type PathFingerprint string

func (pf PathFingerprint) String() string {
	return fmt.Sprintf("%x", []byte(pf))
}

// Fingerprint uniquely identifies the path based on the sequence of
// ASes and BRs, i.e. by its PathInterfaces.
// Other metadata, such as MTU or NextHop have no effect on the fingerprint.
// Returns empty string for paths where the interfaces list is not available.
func Fingerprint(path []PathInterface) PathFingerprint {
	if len(path) == 0 {
		return ""
	}
	h := sha256.New()
	for _, intf := range path {
		if err := binary.Write(h, binary.BigEndian, intf.IA); err != nil {
			// hash.Hash.Write may never error.
			// The type check in binary.Write should also pass for addr.IA.
			panic(err)
		}
		if err := binary.Write(h, binary.BigEndian, intf.ID); err != nil {
			panic(err)
		}
	}
	return PathFingerprint(h.Sum(nil))
}

// partialPath is a path object with incomplete metadata. It is used as a
// temporary solution where a full path cannot be reconstituted from other
// objects, notably snet.UDPAddr and snet.SVCAddr.
type partialPath struct {
	dataplane   DataplanePath
	underlay    *net.UDPAddr
	source      addr.IA
	destination addr.IA
}

func (p *partialPath) UnderlayNextHop() *net.UDPAddr {
	return p.underlay
}

func (p *partialPath) Dataplane() DataplanePath {
	return p.dataplane
}

func (p *partialPath) Source() addr.IA {
	return p.source
}

func (p *partialPath) Destination() addr.IA {
	return p.destination
}

func (p *partialPath) Metadata() *PathMetadata {
	return nil
}
