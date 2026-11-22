// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package models

import (
	"fmt"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

const (
	dashboardMaxWidgets = 50
	dashboardGridCols   = 12
	dashboardMaxRows    = 1000
)

// The widget types of a dashboard.
const (
	WidgetTypeNumber         = "number"
	WidgetTypeBar            = "bar"
	WidgetTypeDonut          = "donut"
	WidgetTypeLine           = "line"
	WidgetTypeCreatedVsDone  = "created_vs_done"
	WidgetTypeTaskList       = "task_list"
	WidgetTypeWorkload       = "workload"
	WidgetTypeStatusTime     = "status_time"
	WidgetTypeTimeTracking   = "time_tracking"
	WidgetTypeText           = "text"
	widgetTypesDocumentation = "number, bar, donut, line, created_vs_done, task_list, workload, status_time, time_tracking, text"
)

// DashboardWidget is one tile of a dashboard.
type DashboardWidget struct {
	ID          int64          `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this widget. Send the id of an existing widget to keep it when you replace the widgets of a dashboard; omit it or send 0 to create a widget."`
	DashboardID int64          `xorm:"bigint not null INDEX" json:"dashboard_id" readOnly:"true" doc:"The dashboard this widget belongs to."`
	Type        string         `xorm:"varchar(40) not null" json:"type" enum:"number,bar,donut,line,created_vs_done,task_list,workload,status_time,time_tracking,text" doc:"What the widget shows. The config has a shape per type."`
	Title       string         `xorm:"varchar(250) null" json:"title" maxLength:"250" doc:"An optional title shown above the widget."`
	Config      map[string]any `xorm:"json null" json:"config" doc:"The settings of the widget. number: {request, compare}. bar, donut and line: {request}, where request is a report request (POST /reports/tasks) that is checked on save but not run. created_vs_done: {project_id, filter, bucket, from, to}. task_list: {project_id, filter, sort_by, order_by, columns, limit}. workload: {project_id, filter, estimate_field}. status_time: {project_id, from, to}. time_tracking: {from, to, group_by}. text: {text}. On a project dashboard project_id values are set to the project of the dashboard."`
	X           int64          `xorm:"bigint not null default 0" json:"x" minimum:"0" maximum:"11" doc:"The column of the left edge in the 12 column grid, starting at 0."`
	Y           int64          `xorm:"bigint not null default 0" json:"y" minimum:"0" doc:"The row of the top edge in the grid, starting at 0."`
	W           int64          `xorm:"bigint not null default 4" json:"w" minimum:"1" maximum:"12" doc:"The width in grid columns. x + w must not exceed 12."`
	H           int64          `xorm:"bigint not null default 3" json:"h" minimum:"1" doc:"The height in grid rows."`
	Created     time.Time      `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this widget was created. You cannot change this value."`
	Updated     time.Time      `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this widget was last updated. You cannot change this value."`
}

// TableName returns the table name of dashboard widgets.
func (*DashboardWidget) TableName() string {
	return "dashboard_widgets"
}

// Dashboard is a page of widgets. A personal dashboard (no project) belongs to its owner and reports over every project
// they can read; a project dashboard is visible to everyone who can read the project.
type Dashboard struct {
	ID          int64              `xorm:"bigint autoincr not null unique pk" json:"id" param:"dashboard" readOnly:"true" doc:"The unique, numeric id of this dashboard."`
	Title       string             `xorm:"varchar(250) not null" json:"title" minLength:"1" maxLength:"250" doc:"The title of the dashboard."`
	Description string             `xorm:"longtext null" json:"description" doc:"What the dashboard is for."`
	OwnerID     int64              `xorm:"bigint not null INDEX" json:"-"`
	Owner       *user.User         `xorm:"-" json:"owner" readOnly:"true" doc:"The user who created the dashboard."`
	ProjectID   int64              `xorm:"bigint not null default 0 INDEX" json:"project_id" query:"project_id" doc:"0 for a personal dashboard, else the project of a project dashboard. It cannot change after creation. In the list it filters to the dashboards of one project."`
	Position    float64            `xorm:"double not null default 0" json:"position" doc:"Sort order in lists, ascending."`
	Widgets     []*DashboardWidget `xorm:"-" json:"widgets" doc:"The widgets. Null in lists. On update, send the whole list to replace the widgets (widgets with an id are updated, those without one are created and stored widgets that are missing are deleted); null or omitting it keeps them."`
	WidgetCount int64              `xorm:"-" json:"widget_count" readOnly:"true" doc:"The number of widgets, so lists need not load them."`
	Created     time.Time          `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this dashboard was created. You cannot change this value."`
	Updated     time.Time          `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this dashboard was last updated. You cannot change this value."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of dashboards.
func (*Dashboard) TableName() string {
	return "dashboards"
}

func getDashboardByID(s *xorm.Session, id int64) (*Dashboard, error) {
	d := &Dashboard{}
	exists, err := s.Where("id = ?", id).Get(d)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrDashboardDoesNotExist{DashboardID: id}
	}
	return d, nil
}

func loadDashboardWidgets(s *xorm.Session, dashboardID int64) ([]*DashboardWidget, error) {
	widgets := []*DashboardWidget{}
	err := s.Where("dashboard_id = ?", dashboardID).OrderBy("y ASC, x ASC, id ASC").Find(&widgets)
	return widgets, err
}

// loadDashboard returns the dashboard with its widgets and owner.
func loadDashboard(s *xorm.Session, id int64) (*Dashboard, error) {
	d, err := getDashboardByID(s, id)
	if err != nil {
		return nil, err
	}
	if d.Widgets, err = loadDashboardWidgets(s, id); err != nil {
		return nil, err
	}
	d.WidgetCount = int64(len(d.Widgets))
	d.attachOwner(s)
	return d, nil
}

func (d *Dashboard) attachOwner(s *xorm.Session) {
	if d.OwnerID == 0 {
		return
	}
	if u, err := user.GetUserByID(s, d.OwnerID); err == nil {
		d.Owner = u
	}
}

// validate checks the dashboard document: size, grid and the config of every widget. It does not run any report.
func (d *Dashboard) validate(s *xorm.Session, a web.Auth) error {
	if len([]rune(d.Title)) < 1 || len([]rune(d.Title)) > 250 {
		return ErrDashboardInvalid{Param: "title", Reason: "must be between 1 and 250 characters"}
	}
	if len(d.Widgets) > dashboardMaxWidgets {
		return ErrDashboardInvalid{Param: "widgets", Reason: fmt.Sprintf("a dashboard has at most %d widgets", dashboardMaxWidgets)}
	}
	for i, w := range d.Widgets {
		if w == nil {
			return ErrDashboardInvalid{Param: fmt.Sprintf("widgets[%d]", i), Reason: "must not be null"}
		}
		if err := w.validate(s, a, d.ProjectID, fmt.Sprintf("widgets[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

// Create validates and stores a dashboard with its widgets. The creator becomes its owner.
// @Summary Create a dashboard
func (d *Dashboard) Create(s *xorm.Session, a web.Auth) error {
	ownerID, err := ownerOf(a)
	if err != nil {
		return err
	}
	if err = d.validate(s, a); err != nil {
		return err
	}
	d.ID = 0
	d.OwnerID = ownerID
	if _, err = s.Insert(d); err != nil {
		return err
	}
	if err = d.insertWidgets(s, d.Widgets); err != nil {
		return err
	}
	return d.reload(s)
}

func (d *Dashboard) reload(s *xorm.Session) error {
	stored, err := loadDashboard(s, d.ID)
	if err != nil {
		return err
	}
	*d = *stored
	return nil
}

func (d *Dashboard) insertWidgets(s *xorm.Session, widgets []*DashboardWidget) error {
	for _, w := range widgets {
		w.ID = 0
		w.DashboardID = d.ID
		if _, err := s.Insert(w); err != nil {
			return err
		}
	}
	return nil
}

// Update replaces the title, description and position and, when widgets are sent, the whole widget list: widgets with
// an id of this dashboard are updated, widgets without an id are created and stored widgets missing from the list are
// deleted. The project of a dashboard cannot change.
// @Summary Update a dashboard
func (d *Dashboard) Update(s *xorm.Session, a web.Auth) error {
	existing, err := getDashboardByID(s, d.ID)
	if err != nil {
		return err
	}
	if d.ProjectID != 0 && d.ProjectID != existing.ProjectID {
		return ErrDashboardInvalid{Param: "project_id", Reason: "cannot be changed after creation"}
	}
	d.ProjectID = existing.ProjectID
	if err = d.validate(s, a); err != nil {
		return err
	}

	if _, err = s.ID(d.ID).Cols("title", "description", "position").Update(d); err != nil {
		return err
	}
	if d.Widgets != nil {
		if err = d.replaceWidgets(s); err != nil {
			return err
		}
	}
	return d.reload(s)
}

func (d *Dashboard) replaceWidgets(s *xorm.Session) error {
	stored, err := loadDashboardWidgets(s, d.ID)
	if err != nil {
		return err
	}
	storedIDs := make(map[int64]bool, len(stored))
	for _, w := range stored {
		storedIDs[w.ID] = true
	}

	seen := map[int64]bool{}
	for _, w := range d.Widgets {
		if w.ID == 0 {
			continue
		}
		if !storedIDs[w.ID] || seen[w.ID] {
			return ErrDashboardWidgetDoesNotExist{DashboardID: d.ID, WidgetID: w.ID}
		}
		seen[w.ID] = true
	}

	for _, w := range d.Widgets {
		w.DashboardID = d.ID
		if w.ID == 0 {
			if _, err = s.Insert(w); err != nil {
				return err
			}
			continue
		}
		if _, err = s.ID(w.ID).Cols("type", "title", "config", "x", "y", "w", "h").Update(w); err != nil {
			return err
		}
	}

	removed := []int64{}
	for id := range storedIDs {
		if !seen[id] {
			removed = append(removed, id)
		}
	}
	if len(removed) > 0 {
		if _, err = s.Where(builder.In("id", removed)).Delete(&DashboardWidget{}); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a dashboard with its widgets.
// @Summary Delete a dashboard
func (d *Dashboard) Delete(s *xorm.Session, _ web.Auth) error {
	if _, err := s.Where("dashboard_id = ?", d.ID).Delete(&DashboardWidget{}); err != nil {
		return err
	}
	_, err := s.ID(d.ID).Delete(&Dashboard{})
	return err
}

// ReadOne returns one dashboard; CanRead already loaded it with its widgets.
// @Summary Get a dashboard
func (d *Dashboard) ReadOne(_ *xorm.Session, _ web.Auth) error {
	return nil
}

// readableDashboardsCond selects the personal dashboards of a user and the dashboards of the projects they can read.
func readableDashboardsCond(s *xorm.Session, a web.Auth) (builder.Cond, error) {
	accessible, err := accessibleProjectIDsCond(s, a, "dashboards.project_id")
	if err != nil {
		return nil, err
	}
	cond := builder.And(builder.Gt{"dashboards.project_id": 0}, accessible)
	if u, is := a.(*user.User); is && u.ID > 0 {
		cond = builder.Or(cond, builder.And(builder.Eq{"dashboards.project_id": 0}, builder.Eq{"dashboards.owner_id": u.ID}))
	}
	return cond, nil
}

// ReadAll lists the personal dashboards of the user and the dashboards of the projects they can read, without widgets.
// @Summary List dashboards
func (d *Dashboard) ReadAll(s *xorm.Session, a web.Auth, search string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	cond, err := readableDashboardsCond(s, a)
	if err != nil {
		return nil, 0, 0, err
	}
	if d.ProjectID > 0 {
		cond = builder.And(cond, builder.Eq{"dashboards.project_id": d.ProjectID})
	}
	if search != "" {
		cond = builder.And(cond, db.ILIKE("dashboards.title", search))
	}

	total, err := s.Where(cond).Count(&Dashboard{})
	if err != nil {
		return nil, 0, 0, err
	}
	dashboards := []*Dashboard{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("dashboards.position ASC, dashboards.id ASC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&dashboards); err != nil {
		return nil, 0, 0, err
	}

	if err = attachWidgetCounts(s, dashboards); err != nil {
		return nil, 0, 0, err
	}
	return dashboards, len(dashboards), total, nil
}

func attachWidgetCounts(s *xorm.Session, dashboards []*Dashboard) error {
	if len(dashboards) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(dashboards))
	byID := make(map[int64]*Dashboard, len(dashboards))
	owners := make([]int64, 0, len(dashboards))
	for _, d := range dashboards {
		ids = append(ids, d.ID)
		byID[d.ID] = d
		owners = append(owners, d.OwnerID)
	}
	rows, err := s.QueryInterface(builder.
		Select("dashboard_id, COUNT(*) AS c").
		From("dashboard_widgets").
		Where(builder.In("dashboard_id", ids)).
		GroupBy("dashboard_id"))
	if err != nil {
		return err
	}
	for _, row := range rows {
		if d, ok := byID[reportInt(row["dashboard_id"])]; ok {
			d.WidgetCount = reportInt(row["c"])
		}
	}
	users, err := user.GetUsersByIDs(s, owners)
	if err != nil {
		return err
	}
	for _, d := range dashboards {
		d.Owner = users[d.OwnerID]
	}
	return nil
}

// DashboardLayoutItem is the position of one widget.
type DashboardLayoutItem struct {
	ID int64 `json:"id" minimum:"1" doc:"The widget."`
	X  int64 `json:"x" minimum:"0" maximum:"11" doc:"The column of the left edge, starting at 0."`
	Y  int64 `json:"y" minimum:"0" doc:"The row of the top edge, starting at 0."`
	W  int64 `json:"w" minimum:"1" maximum:"12" doc:"The width in grid columns."`
	H  int64 `json:"h" minimum:"1" doc:"The height in grid rows."`
}

// DashboardLayout moves and resizes widgets without sending their configuration, for drag and resize.
type DashboardLayout struct {
	DashboardID int64                  `json:"-" param:"dashboard"`
	Items       []*DashboardLayoutItem `json:"items" doc:"The new positions. Widgets that are not listed keep theirs."`

	web.CRUDable    `json:"-"`
	web.Permissions `json:"-"`
}

// Update stores the positions. Every widget has to belong to the dashboard.
// @Summary Update the layout of a dashboard
func (l *DashboardLayout) Update(s *xorm.Session, _ web.Auth) error {
	stored, err := loadDashboardWidgets(s, l.DashboardID)
	if err != nil {
		return err
	}
	storedIDs := make(map[int64]bool, len(stored))
	for _, w := range stored {
		storedIDs[w.ID] = true
	}

	seen := map[int64]bool{}
	for i, item := range l.Items {
		if item == nil {
			return ErrDashboardInvalid{Param: fmt.Sprintf("items[%d]", i), Reason: "must not be null"}
		}
		if err = validateWidgetGrid(item.X, item.Y, item.W, item.H, fmt.Sprintf("items[%d]", i)); err != nil {
			return err
		}
		if !storedIDs[item.ID] {
			return ErrDashboardWidgetDoesNotExist{DashboardID: l.DashboardID, WidgetID: item.ID}
		}
		if seen[item.ID] {
			return ErrDashboardInvalid{Param: fmt.Sprintf("items[%d].id", i), Reason: "is listed twice"}
		}
		seen[item.ID] = true
	}

	for _, item := range l.Items {
		if _, err = s.ID(item.ID).Cols("x", "y", "w", "h").Update(&DashboardWidget{X: item.X, Y: item.Y, W: item.W, H: item.H}); err != nil {
			return err
		}
	}
	// The dashboard changed too: its ETag follows its update time.
	_, err = s.ID(l.DashboardID).Cols("updated").Update(&Dashboard{})
	return err
}

func validateWidgetGrid(x, y, w, h int64, path string) error {
	switch {
	case x < 0 || x >= dashboardGridCols:
		return ErrDashboardInvalid{Param: path + ".x", Reason: fmt.Sprintf("must be between 0 and %d", dashboardGridCols-1)}
	case w < 1 || w > dashboardGridCols:
		return ErrDashboardInvalid{Param: path + ".w", Reason: fmt.Sprintf("must be between 1 and %d", dashboardGridCols)}
	case x+w > dashboardGridCols:
		return ErrDashboardInvalid{Param: path + ".w", Reason: fmt.Sprintf("x + w must not exceed %d columns", dashboardGridCols)}
	case y < 0 || y > dashboardMaxRows:
		return ErrDashboardInvalid{Param: path + ".y", Reason: fmt.Sprintf("must be between 0 and %d", dashboardMaxRows)}
	case h < 1 || h > dashboardMaxRows:
		return ErrDashboardInvalid{Param: path + ".h", Reason: fmt.Sprintf("must be between 1 and %d", dashboardMaxRows)}
	}
	return nil
}

// deleteDashboardsOfProject removes the dashboards of a project with their widgets. It runs when a project is purged.
func deleteDashboardsOfProject(s *xorm.Session, projectID int64) error {
	ids := builder.Select("id").From("dashboards").Where(builder.Eq{"project_id": projectID})
	if _, err := s.Where(builder.In("dashboard_id", ids)).Delete(&DashboardWidget{}); err != nil {
		return err
	}
	_, err := s.Where("project_id = ?", projectID).Delete(&Dashboard{})
	return err
}

// deletePersonalDashboardsOfUser removes the personal dashboards of a user. It runs when the user is deleted.
func deletePersonalDashboardsOfUser(s *xorm.Session, userID int64) error {
	ids := builder.Select("id").From("dashboards").Where(builder.And(builder.Eq{"owner_id": userID}, builder.Eq{"project_id": 0}))
	if _, err := s.Where(builder.In("dashboard_id", ids)).Delete(&DashboardWidget{}); err != nil {
		return err
	}
	_, err := s.Where(builder.And(builder.Eq{"owner_id": userID}, builder.Eq{"project_id": 0})).Delete(&Dashboard{})
	return err
}
