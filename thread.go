package thread

import (
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-ipld-cbor"
	"github.com/ipfs/go-ipld-format"
	h "github.com/libp2p/go-libp2p-core/host"
	mh "github.com/multiformats/go-multihash"
	"github.com/textileio/go-textile-core/crypto"
	"github.com/textileio/go-textile-core/threads"
	"github.com/textileio/go-textile-wallet/account"
)

type Thread struct {
	host h.Host
	node format.Node

	schema threads.Schema
	intent threads.ThreadIntent
	roles  threads.Roles
	seed   []byte

	reader     []byte
	replicator []byte

	name string
}

func New(host h.Host, opts ...Option) (*Thread, error) {
	obj := make(map[string]interface{})
	settings := Options(opts...)

	// @todo: decode into actual schema object
	var s map[string]interface{}
	err := cbornode.DecodeInto(settings.Schema.RawData(), &s)
	if err != nil {
		return nil, err
	}
	obj["schema"] = s

	// @todo: decode into actual roles object
	var r map[string]interface{}
	err = cbornode.DecodeInto(settings.Roles.RawData(), &r)
	if err != nil {
		return nil, err
	}
	obj["roles"] = r

	obj["intent"] = settings.Intent

	// generate new keys
	reader, err := crypto.GenerateAESKey()
	if err != nil {
		return nil, err
	}
	replicator, err := crypto.GenerateAESKey()
	if err != nil {
		return nil, err
	}

	// seed randomizes the thread
	seed, err := mh.Sum(append(reader, replicator...), mh.SHA2_256, -1)
	if err != nil {
		return nil, err
	}
	obj["seed"] = seed

	node, err := cbornode.WrapObject(obj, mh.SHA2_256, -1)
	if err != nil {
		return nil, err
	}

	return &Thread{
		host:       host,
		node:       node,
		seed:       seed,
		reader:     reader,
		replicator: replicator,
		name:       settings.Name,
	}, nil
}

func (t *Thread) Heads() []cid.Cid {
	return nil
}

func (t *Thread) GetName() string {
	return t.name
}

func (t *Thread) SetName(val string) {
	t.name = val
}

func (t *Thread) CreateInvite() (cid.Cid, error) {
	return cid.Undef, nil
}

func (t *Thread) Invite(account.Account) error {
	return nil
}

func (t *Thread) Join() error {
	return nil
}

func (t *Thread) Leave() error {
	return nil
}

func (t *Thread) Logs() []threads.Log {
	return nil
}

func (t *Thread) Write(threads.Node) (cid.Cid, error) {
	return cid.Undef, nil
}

func (t *Thread) Listen() <-chan threads.Node {
	return nil
}

func (t *Thread) Fork() (threads.Thread, error) {
	return nil, nil
}