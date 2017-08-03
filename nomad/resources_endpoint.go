package nomad

import (
	"fmt"
	"github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
)

// truncateLimit is the maximum number of matches that will be returned for a
// prefix for a specific context
const truncateLimit = 20

// Resource endpoint is used to lookup matches for a given prefix and context
type Resources struct {
	srv *Server
}

// getMatches extracts matches for an iterator, and returns a list of ids for
// these matches.
func getMatches(iter memdb.ResultIterator) ([]string, bool) {
	var matches []string
	isTruncated := false

	for i := 0; i < truncateLimit; i++ {
		raw := iter.Next()
		if raw == nil {
			break
		}

		getID := func(i interface{}) (string, error) {
			switch i.(type) {
			case *structs.Job:
				return i.(*structs.Job).ID, nil
			case *structs.Evaluation:
				return i.(*structs.Evaluation).ID, nil
			case *structs.Allocation:
				return i.(*structs.Allocation).ID, nil
			case *structs.Node:
				return i.(*structs.Node).ID, nil
			default:
				return "", fmt.Errorf("invalid context")
			}
		}

		id, err := getID(raw)
		if err != nil {
			continue
		}

		matches = append(matches, id)
	}

	if iter.Next() != nil {
		isTruncated = true
	}

	return matches, isTruncated
}

// getResourceIter takes a context and returns a memdb iterator specific to
// that context
func getResourceIter(context, prefix string, ws memdb.WatchSet, state *state.StateStore) (memdb.ResultIterator, error) {
	switch context {
	case "job":
		return state.JobsByIDPrefix(ws, prefix)
	case "eval":
		return state.EvalsByIDPrefix(ws, prefix)
	case "alloc":
		return state.AllocsByIDPrefix(ws, prefix)
	case "node":
		return state.NodesByIDPrefix(ws, prefix)
	default:
		return nil, fmt.Errorf("invalid context")
	}
}

// List is used to list the resouces registered in the system that matches the
// given prefix. Resources are jobs, evaluations, allocations, and/or nodes.
func (r *Resources) List(args *structs.ResourcesRequest,
	reply *structs.ResourcesResponse) error {
	reply.Matches = make(map[string][]string)
	reply.Truncations = make(map[string]bool)

	// Setup the blocking query
	opts := blockingOptions{
		queryMeta: &reply.QueryMeta,
		queryOpts: &structs.QueryOptions{},
		run: func(ws memdb.WatchSet, state *state.StateStore) error {

			iters := make(map[string]memdb.ResultIterator)

			if args.Context != "" {
				iter, err := getResourceIter(args.Context, args.Prefix, ws, state)
				if err != nil {
					return err
				}
				iters[args.Context] = iter
			} else {
				for _, e := range []string{"alloc", "node", "job", "eval"} {
					iter, err := getResourceIter(e, args.Prefix, ws, state)
					if err != nil {
						return err
					}
					iters[e] = iter
				}
			}

			// return jobs matching given prefix
			for k, v := range iters {
				res, isTrunc := getMatches(v)
				reply.Matches[k] = res
				reply.Truncations[k] = isTrunc
			}

			// Use the last index that affected the table
			index, err := state.Index(args.Context)
			if err != nil {
				return err
			}
			reply.Index = index

			return nil
		}}
	return r.srv.blockingRPC(&opts)
}
