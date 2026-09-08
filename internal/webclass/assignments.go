package webclass

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type SubmissionStatus string

const (
	SubmissionUnknown   SubmissionStatus = "unknown"
	SubmissionPending   SubmissionStatus = "pending"
	SubmissionSubmitted SubmissionStatus = "submitted"
	SubmissionResubmit  SubmissionStatus = "resubmit"
)

type Assignment struct {
	CourseID         string
	CourseName       string
	ID               string
	Title            string
	Category         string
	Deadline         time.Time
	HasDeadline      bool
	DeadlineText     string
	SubmissionStatus SubmissionStatus
	ScoreText        string
	Score            float64
	MaxScore         float64
	HasScore         bool
	HasMaxScore      bool
}

type assignmentScore struct {
	Submitted   bool
	Raw         string
	Score       float64
	MaxScore    float64
	HasScore    bool
	HasMaxScore bool
}

type scoreColumns struct {
	Score int
	Max   int
}

var (
	assignmentDeadlinePattern = regexp.MustCompile(`締め切り[：:]\s*(\d{4})/(\d{2})/(\d{2})\s+(\d{2}):(\d{2})`)
	assignmentRangePattern    = regexp.MustCompile(`\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}\s*-\s*(\d{4})/(\d{2})/(\d{2})\s+(\d{2}):(\d{2})`)
	scorePairPattern          = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*(?:点)?\s*/\s*(-?\d+(?:\.\d+)?)\s*(?:点)?\s*$`)
	scoreNumberPattern        = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*(?:点)?\s*$`)
)

// Assignments reads a course's contents list and score sheet without opening
// any material. Entries categorized exactly as 資料 are excluded.
func (c *Client) Assignments(course Course) ([]Assignment, error) {
	indexURL := c.courseIndexURL(course.ID)
	if course.URL != "" && course.URL != indexURL {
		resp, err := c.get(course.URL)
		if err != nil {
			return nil, fmt.Errorf("enter course %s: %w", course.ID, err)
		}
		resp.Body.Close()
	}

	doc, _, err := c.document(indexURL)
	if err != nil {
		return nil, fmt.Errorf("read course %s: %w", course.ID, err)
	}

	scores, scoresAvailable, err := c.assignmentScores(course.ID)
	if err != nil {
		return nil, err
	}

	var assignments []Assignment
	doc.Find(".cl-contentsList_listGroupItem").Each(func(_ int, row *goquery.Selection) {
		category := normalizedText(row.Find(".cl-contentsList_categoryLabel").First())
		if category == "資料" {
			return
		}

		nameNode := row.Find(".cm-contentsList_contentName").First()
		title := assignmentRowTitle(nameNode)
		if title == "" {
			title = strings.TrimSpace(row.AttrOr("data-contents-name", ""))
		}
		if title == "" {
			return
		}

		id := strings.TrimSpace(row.AttrOr("data-contents-id", ""))
		deadline, hasDeadline, deadlineText := assignmentDeadline(row)
		score, hasScoreRecord := scores[title]

		status := SubmissionUnknown
		if assignmentNeedsResubmit(row) {
			status = SubmissionResubmit
		} else if hasScoreRecord {
			if score.Submitted {
				status = SubmissionSubmitted
			} else {
				status = SubmissionPending
			}
		} else if strings.Contains(normalizedText(row), "提出済") {
			status = SubmissionSubmitted
		} else if scoresAvailable {
			// A score sheet was available, but this title was absent. Do not infer
			// that an arbitrary survey/self-study item has been submitted.
			status = SubmissionUnknown
		}

		assignments = append(assignments, Assignment{
			CourseID: course.ID, CourseName: course.Name, ID: id,
			Title: title, Category: category,
			Deadline: deadline, HasDeadline: hasDeadline, DeadlineText: deadlineText,
			SubmissionStatus: status,
			ScoreText: score.Raw, Score: score.Score, MaxScore: score.MaxScore,
			HasScore: score.HasScore, HasMaxScore: score.HasMaxScore,
		})
	})

	sortAssignments(assignments)
	return assignments, nil
}

func (c *Client) assignmentScores(courseID string) (map[string]assignmentScore, bool, error) {
	scoresURL := c.Base.ResolveReference(mustParse("course.php/" + courseID + "/scores")).String()
	doc, _, err := c.document(scoresURL)
	if err != nil {
		if err == ErrNotAuthenticated {
			return nil, false, err
		}
		// Some WebClass installations may not expose this page. Listing the
		// assignments is still useful; submission/score state will be unknown.
		return map[string]assignmentScore{}, false, nil
	}

	table := doc.Find("#PersonalScoreSheet").First()
	if table.Length() == 0 {
		return map[string]assignmentScore{}, false, nil
	}

	columns := detectScoreColumns(table)
	result := map[string]assignmentScore{}
	table.Find("tr").Each(func(_ int, row *goquery.Selection) {
		title := normalizedText(row.Find(".contents-title").First())
		if title == "" {
			return
		}

		rawScore := cellTextAt(row, columns.Score)
		if rawScore == "" {
			rawScore = normalizedText(row.Find("td").First())
		}
		record := parseAssignmentScore(rawScore)

		if columns.Max >= 0 && columns.Max != columns.Score {
			if rawMax := cellTextAt(row, columns.Max); rawMax != "" {
				if max, ok := parseSingleScore(rawMax); ok {
					record.MaxScore = max
					record.HasMaxScore = true
				}
			}
		}
		result[title] = record
	})
	return result, true, nil
}

func detectScoreColumns(table *goquery.Selection) scoreColumns {
	columns := scoreColumns{Score: -1, Max: -1}
	table.Find("tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		cells := row.ChildrenFiltered("th, td")
		rowScore, rowMax := -1, -1
		cells.Each(func(i int, cell *goquery.Selection) {
			text := normalizedText(cell)
			if rowScore < 0 && isScoreHeader(text) {
				rowScore = i
			}
			if rowMax < 0 && isMaxScoreHeader(text) {
				rowMax = i
			}
		})
		if rowScore >= 0 || rowMax >= 0 {
			columns.Score, columns.Max = rowScore, rowMax
			return false
		}
		return true
	})
	return columns
}

func isScoreHeader(text string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(text), ""))
	return strings.Contains(compact, "得点") || strings.Contains(compact, "点数") || compact == "score" || strings.HasPrefix(compact, "score/")
}

func isMaxScoreHeader(text string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(text), ""))
	return strings.Contains(compact, "満点") || strings.Contains(compact, "配点") || strings.Contains(compact, "最高点") || compact == "max" || strings.Contains(compact, "maxscore")
}

func cellTextAt(row *goquery.Selection, index int) string {
	if index < 0 {
		return ""
	}
	cells := row.ChildrenFiltered("th, td")
	if index >= cells.Length() {
		return ""
	}
	return normalizedText(cells.Eq(index))
}

func parseAssignmentScore(text string) assignmentScore {
	text = strings.Join(strings.Fields(text), " ")
	record := assignmentScore{Raw: text, Submitted: text != "" && text != "未"}
	if m := scorePairPattern.FindStringSubmatch(text); len(m) == 3 {
		if score, err := strconv.ParseFloat(m[1], 64); err == nil {
			record.Score, record.HasScore = score, true
		}
		if max, err := strconv.ParseFloat(m[2], 64); err == nil {
			record.MaxScore, record.HasMaxScore = max, true
		}
		return record
	}
	if score, ok := parseSingleScore(text); ok {
		record.Score, record.HasScore = score, true
	}
	return record
}

func parseSingleScore(text string) (float64, bool) {
	m := scoreNumberPattern.FindStringSubmatch(strings.Join(strings.Fields(text), " "))
	if len(m) != 2 {
		return 0, false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	return n, err == nil
}

func assignmentRowTitle(nameNode *goquery.Selection) string {
	if nameNode == nil || nameNode.Length() == 0 {
		return ""
	}
	inner := nameNode.Find("a, div:not(.cl-contentsList_new)").First()
	if inner.Length() > 0 {
		if title := normalizedText(inner); title != "" {
			return title
		}
	}
	return normalizedText(nameNode)
}

func assignmentDeadline(row *goquery.Selection) (time.Time, bool, string) {
	var deadline time.Time
	var text string
	found := false
	row.Find(".cm-contentsList_contentDetailListItemData").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := normalizedText(s)
		if raw == "" {
			return true
		}
		if parsed, matched, ok := parseAssignmentDeadline(raw); ok {
			deadline, text, found = parsed, matched, true
			return false
		}
		return true
	})
	return deadline, found, text
}

func parseAssignmentDeadline(text string) (time.Time, string, bool) {
	for _, pattern := range []*regexp.Regexp{assignmentDeadlinePattern, assignmentRangePattern} {
		m := pattern.FindStringSubmatch(text)
		if len(m) != 6 {
			continue
		}
		parsed, err := time.ParseInLocation("2006/01/02 15:04", strings.Join([]string{m[1] + "/" + m[2] + "/" + m[3], m[4] + ":" + m[5]}, " "), time.Local)
		if err != nil {
			continue
		}
		return parsed, parsed.Format("2006/01/02 15:04"), true
	}
	return time.Time{}, "", false
}

func assignmentNeedsResubmit(row *goquery.Selection) bool {
	needs := false
	row.Find(".text-danger").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.Contains(normalizedText(s), "再提出") {
			needs = true
			return false
		}
		return true
	})
	return needs
}

func sortAssignments(assignments []Assignment) {
	sort.SliceStable(assignments, func(i, j int) bool {
		a, b := assignments[i], assignments[j]
		if a.HasDeadline != b.HasDeadline {
			return a.HasDeadline
		}
		if a.HasDeadline && !a.Deadline.Equal(b.Deadline) {
			return a.Deadline.Before(b.Deadline)
		}
		if a.CourseName != b.CourseName {
			return a.CourseName < b.CourseName
		}
		return a.Title < b.Title
	})
}
