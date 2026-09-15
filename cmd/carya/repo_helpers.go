package main

import (
	"fmt"
	"os"

	"carya/internal/repository"
)

const notCaryaRepoMessage = "Error: Not a Carya repository. Run 'carya init' first.\n"

func mustRepo() *repository.Repository {
	repo, err := repository.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return repo
}

func mustInitializedRepo() *repository.Repository {
	repo := mustRepo()
	if !repo.Exists() {
		fmt.Fprintf(os.Stderr, notCaryaRepoMessage)
		os.Exit(1)
	}
	return repo
}
