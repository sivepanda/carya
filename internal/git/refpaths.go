package git

import "fmt"

const defaultTeamRemote = "origin"

func UserTreeRefPath(userID string) string {
	return fmt.Sprintf("refs/carya/users/%s/tree", userID)
}

func RemoteUserTreeRefPath(remote, userID string) string {
	return fmt.Sprintf("refs/remotes/%s/carya/users/%s/tree", remote, userID)
}

func RemoteUserRefsPrefix(remote string) string {
	return "refs/remotes/" + remote + "/carya/users/"
}

func LocalUserRefsPrefix() string {
	return "refs/carya/users/"
}
