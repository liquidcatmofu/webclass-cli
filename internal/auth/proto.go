package auth

import "github.com/go-rod/rod/lib/proto"

func protoTarget(rawURL string) proto.TargetCreateTarget {
	return proto.TargetCreateTarget{URL: rawURL}
}
