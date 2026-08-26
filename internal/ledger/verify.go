package ledger

import "context"

func VerifyEntry(ctx context.Context, entry Entry) (bool, error) {
	start, args, live, err := processIdentity(ctx, entry.PID)
	if err != nil || !live {
		return false, err
	}
	return start == entry.PSLstart && args == entry.PSArgs, nil
}
