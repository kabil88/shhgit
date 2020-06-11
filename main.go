package main

import (
	"bufio"
	"bytes"
	"github.com/eth0izzle/shhgit/core"
	"github.com/fatih/color"
	"os"
	"regexp"
	"strings"
)

var session = core.GetSession()

func ProcessRepositories() {
	threadNum := *session.Options.Threads

	for i := 0; i < threadNum; i++ {
		go func(tid int) {

			for {
				repositoryId := <-session.Repositories
				repo, err := core.GetRepository(session, repositoryId)

				if err != nil {
					session.Log.Warn("Failed to retrieve repository %d: %s", repositoryId, err)
					continue
				}

				if repo.GetPermissions()["pull"] &&
					uint(repo.GetStargazersCount()) >= *session.Options.MinimumStars &&
					uint(repo.GetSize()) < *session.Options.MaximumRepositorySize {

					processRepositoryOrGist(repo.GetCloneURL())
				}
			}
		}(i)
	}
}

func ProcessGists() {
	threadNum := *session.Options.Threads

	for i := 0; i < threadNum; i++ {
		go func(tid int) {
			for {
				gistUrl := <-session.Gists
				processRepositoryOrGist(gistUrl)
			}
		}(i)
	}
}

func processRepositoryOrGist(url string) {
	var (
		matchedAny bool = false
	)

	dir := core.GetTempDir(core.GetHash(url))
	_, err := core.CloneRepository(session, url, dir)

	if err != nil {
		session.Log.Debug("[%s] Cloning failed: %s", url, err.Error())
		os.RemoveAll(dir)
		return
	}

	session.Log.Debug("[%s] Cloning in to %s", url, strings.Replace(dir, *session.Options.TempDirectory, "", -1))
	matchedAny = checkSignatures(dir, url)
	if !matchedAny {
		os.RemoveAll(dir)
	}
}

func checkSearchQuery(file core.MatchFile) (matches []string) {

	queryRegex := regexp.MustCompile(*session.Options.SearchQuery)
	for _, match := range queryRegex.FindAllSubmatch(file.Contents, -1) {
		matches = append(matches, string(match[0]))
	}
	return
}

func checkSignaturesForFile(file core.MatchFile, relativeFileName string, url string) (matchedAny bool) {
	var matches []string

	for _, signature := range session.Signatures {
		if matched, part := signature.Match(file); matched {
			matchedAny = true

			if part == core.PartContents {
				if matches = signature.GetContentsMatches(file); matches != nil {
					count := len(matches)
					m := strings.Join(matches, ", ")
					session.Log.Important("%d %s for %s in file %s: %s", count, core.Pluralize(count, "match", "matches"), color.GreenString(signature.Name()), url+relativeFileName, color.YellowString(m))
					session.WriteToCsv([]string{url, signature.Name(), relativeFileName, m})
				}
			} else {
				if *session.Options.PathChecks {
					session.Log.Important("Matching file %s for %s", url+relativeFileName, color.GreenString(signature.Name()))
					session.WriteToCsv([]string{url, signature.Name(), relativeFileName, ""})
				}

				if *session.Options.EntropyThreshold > 0 && file.CanCheckEntropy() {
					scanner := bufio.NewScanner(bytes.NewReader(file.Contents))

					for scanner.Scan() {
						line := scanner.Text()

						if len(line) > 6 && len(line) < 100 {
							entropy := core.GetEntropy(scanner.Text())

							if entropy >= *session.Options.EntropyThreshold {
								session.Log.Important("Potential secret in %s = %s", url+relativeFileName, color.GreenString(scanner.Text()))
								session.WriteToCsv([]string{url, signature.Name(), relativeFileName, scanner.Text()})
							}
						}
					}
				}
			}
		}
	}
	return
}

func checkSignatures(dir string, url string) (matchedAny bool) {
	url = url[:len(url)-4] + "/" + "blob/master"

	if *session.Options.SearchQuery != "" && *session.Options.KeepSignatures {
		matchedQuery := false
		for _, file := range core.GetMatchingFiles(dir) {
			var (
				matches          []string
				relativeFileName string
			)

			if strings.Contains(dir, *session.Options.TempDirectory) {
				relativeFileName = strings.Replace(file.Path, *session.Options.TempDirectory, "", -1)
			} else {
				relativeFileName = strings.Replace(file.Path, dir, "", -1)
			}
			relativeFileName = relativeFileName[41:]

			var searchQueryMatches []string
			searchQueryMatches = checkSearchQuery(file)
			if searchQueryMatches != nil {
				matches = append(matches, searchQueryMatches...)
				matchedQuery = true
			}
			if matches != nil {
				count := len(matches)
				m := strings.Join(matches, ", ")
				session.Log.Warn("%d %s for %s in file %s: %s", count, core.Pluralize(count, "match", "matches"), color.GreenString("Search Query"), url+relativeFileName, color.YellowString(m))
				session.WriteToCsv([]string{url, "Search Query", relativeFileName, m})
			}
		}
		if matchedQuery {
			for _, file := range core.GetMatchingFiles(dir) {

				var relativeFileName string

				if strings.Contains(dir, *session.Options.TempDirectory) {
					relativeFileName = strings.Replace(file.Path, *session.Options.TempDirectory, "", -1)
				} else {
					relativeFileName = strings.Replace(file.Path, dir, "", -1)
				}
				relativeFileName = relativeFileName[41:]

				matchedAny = checkSignaturesForFile(file, relativeFileName, url)
			}
		}

		os.RemoveAll(dir)

	} else {
		for _, file := range core.GetMatchingFiles(dir) {

			var (
				matches          []string
				relativeFileName string
			)

			if strings.Contains(dir, *session.Options.TempDirectory) {
				relativeFileName = strings.Replace(file.Path, *session.Options.TempDirectory, "", -1)
			} else {
				relativeFileName = strings.Replace(file.Path, dir, "", -1)
			}
			relativeFileName = relativeFileName[41:]

			if *session.Options.SearchQuery != "" {
				var searchQueryMatches []string
				searchQueryMatches = checkSearchQuery(file)
				if searchQueryMatches != nil {
					matches = append(matches, searchQueryMatches...)
				}
				if matches != nil {
					count := len(matches)
					m := strings.Join(matches, ", ")
					session.Log.Important("%d %s for %s in file %s: %s", count, core.Pluralize(count, "match", "matches"), color.GreenString("Search Query"), url+relativeFileName, color.YellowString(m))
					session.WriteToCsv([]string{url, "Search Query", relativeFileName, m})
				}
			} else {
				matchedAny = checkSignaturesForFile(file, relativeFileName, url)
			}

			if !matchedAny {
				os.Remove(file.Path)
			}
		}
	}
	return
}

func main() {
	if session.Options.LocalRun {
		session.Log.Info("Scanning local dir %s with %s v%s. Loaded %d signatures.", *session.Options.Local, core.Name, core.Version, len(session.Signatures))
		rc := 0
		if checkSignatures(*session.Options.Local, *session.Options.Local) {
			rc = 1
		}
		os.Exit(rc)
	} else {
		session.Log.Info("%s v%s started. Loaded %d signatures. Using %d GitHub tokens and %d threads. Work dir: %s", core.Name, core.Version, len(session.Signatures), len(session.Clients), *session.Options.Threads, *session.Options.TempDirectory)

		if *session.Options.SearchQuery != "" && !*session.Options.KeepSignatures {
			session.Log.Important("Search Query '%s' given. Only returning matching results.", *session.Options.SearchQuery)
		}

		go core.GetRepositories(session)
		go ProcessRepositories()

		if *session.Options.ProcessGists {
			go core.GetGists(session)
			go ProcessGists()
		}

		session.Log.Info("Press Ctrl+C to stop and exit.\n")
		select {}
	}
}
