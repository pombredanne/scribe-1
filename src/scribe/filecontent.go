// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// Contributor:
// - Aaron Meihm ameihm@mozilla.com

package scribe

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
)

type FileContent struct {
	Path       string `json:"path"`
	File       string `json:"file"`
	Expression string `json:"expression"`

	matches []contentMatch
}

type contentMatch struct {
	path    string
	matches []matchLine
}

type matchLine struct {
	fullmatch string
	groups    []string
}

func (f *FileContent) validate() error {
	if len(f.Path) == 0 {
		return fmt.Errorf("filecontent path must be set")
	}
	if len(f.File) == 0 {
		return fmt.Errorf("filecontent file must be set")
	}
	_, err := regexp.Compile(f.File)
	if err != nil {
		return err
	}
	if len(f.Expression) == 0 {
		return fmt.Errorf("filecontent expression must be set")
	}
	_, err = regexp.Compile(f.Expression)
	if err != nil {
		return err
	}
	return nil
}

func (f *FileContent) isModifier() bool {
	return false
}

func (f *FileContent) expandVariables(v []Variable) {
	f.Path = variableExpansion(v, f.Path)
	f.File = variableExpansion(v, f.File)
}

func (f *FileContent) getCriteria() (ret []EvaluationCriteria) {
	for _, x := range f.matches {
		for _, y := range x.matches {
			for _, z := range y.groups {
				n := EvaluationCriteria{}
				n.Identifier = x.path
				n.TestValue = z
				ret = append(ret, n)
			}
		}
	}
	return ret
}

func (f *FileContent) prepare() error {
	debugPrint("prepare(): analyzing file system, path %v, file \"%v\"\n", f.Path, f.File)

	sfl := newSimpleFileLocator()
	sfl.root = f.Path
	err := sfl.locate(f.File, true)
	if err != nil {
		return err
	}

	for _, x := range sfl.matches {
		m, err := fileContentCheck(x, f.Expression)
		// XXX These soft errors during preparation are ignored right
		// now, but they should probably be tracked somewhere.
		if err != nil {
			continue
		}
		if m == nil || len(m) == 0 {
			continue
		}

		ncm := contentMatch{}
		ncm.path = x
		ncm.matches = m
		f.matches = append(f.matches, ncm)
		debugPrint("prepare(): content matches in %v\n", ncm.path)
		for _, i := range ncm.matches {
			debugPrint("prepare(): full match: \"%v\"\n", i.fullmatch)
			for j := range i.groups {
				debugPrint("prepare(): group %v: \"%v\"\n", j, i.groups[j])
			}
		}
	}

	return nil
}

type simpleFileLocator struct {
	executed bool
	root     string
	curDepth int
	maxDepth int
	matches  []string
}

func newSimpleFileLocator() (ret simpleFileLocator) {
	// XXX This needs to be fixed to work with Windows.
	ret.root = "/"
	ret.maxDepth = 10
	ret.matches = make([]string, 0)
	return ret
}

func (s *simpleFileLocator) locate(target string, useRegexp bool) error {
	if s.executed {
		return fmt.Errorf("locator has already been executed")
	}
	s.executed = true
	return s.locateInner(target, useRegexp, "")
}

func (s *simpleFileLocator) locateInner(target string, useRegexp bool, path string) error {
	var (
		spath string
		re    *regexp.Regexp
		err   error
	)

	// If processing this directory would result in us exceeding the
	// specified search depth, just ignore it.
	if (s.curDepth + 1) > s.maxDepth {
		return nil
	}

	if useRegexp {
		re, err = regexp.Compile(target)
		if err != nil {
			return err
		}
	}

	s.curDepth++
	defer func() {
		s.curDepth--
	}()

	if path == "" {
		spath = s.root
	} else {
		spath = path
	}
	dirents, err := ioutil.ReadDir(spath)
	if err != nil {
		// If we encounter an error while reading a directory, just
		// ignore it and keep going until we are finished.
		return nil
	}
	for _, x := range dirents {
		fname := filepath.Join(spath, x.Name())
		if x.IsDir() {
			err = s.locateInner(target, useRegexp, fname)
			if err != nil {
				return err
			}
		} else if x.Mode().IsRegular() {
			if !useRegexp {
				if x.Name() == target {
					s.matches = append(s.matches, fname)
				}
			} else {
				if re.MatchString(x.Name()) {
					s.matches = append(s.matches, fname)
				}
			}
		}
	}
	return nil
}

func fileContentCheck(path string, regex string) ([]matchLine, error) {
	re, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		fd.Close()
	}()

	rdr := bufio.NewReader(fd)
	ret := make([]matchLine, 0)
	for {
		ln, err := rdr.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return nil, err
			}
		}
		mtch := re.FindStringSubmatch(ln)
		if len(mtch) > 0 {
			newmatch := matchLine{}
			newmatch.groups = make([]string, 0)
			newmatch.fullmatch = mtch[0]
			for i := 1; i < len(mtch); i++ {
				newmatch.groups = append(newmatch.groups, mtch[i])
			}
			ret = append(ret, newmatch)
		}
	}

	if len(ret) == 0 {
		return nil, nil
	}
	return ret, nil
}
