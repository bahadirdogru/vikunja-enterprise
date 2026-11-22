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
	"math"
	"regexp"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// CustomFieldType is the data type of a custom field. It cannot change after creation.
type CustomFieldType string

// The custom field types.
const (
	CustomFieldTypeText        CustomFieldType = "text"
	CustomFieldTypeLongText    CustomFieldType = "long_text"
	CustomFieldTypeNumber      CustomFieldType = "number"
	CustomFieldTypeCurrency    CustomFieldType = "currency"
	CustomFieldTypeDate        CustomFieldType = "date"
	CustomFieldTypeCheckbox    CustomFieldType = "checkbox"
	CustomFieldTypeSelect      CustomFieldType = "select"
	CustomFieldTypeMultiSelect CustomFieldType = "multi_select"
	CustomFieldTypeUser        CustomFieldType = "user"
	CustomFieldTypeURL         CustomFieldType = "url"
	CustomFieldTypeEmail       CustomFieldType = "email"
	CustomFieldTypeRating      CustomFieldType = "rating"
	CustomFieldTypeProgress    CustomFieldType = "progress"
)

const (
	defaultCustomFieldMaxRating = 5
	maxCustomFieldMaxRating     = 10
	maxCustomFieldPrecision     = 10
	customFieldPositionStep     = 65536
)

var customFieldColorRegex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (t CustomFieldType) isValid() bool {
	switch t {
	case CustomFieldTypeText, CustomFieldTypeLongText, CustomFieldTypeNumber, CustomFieldTypeCurrency,
		CustomFieldTypeDate, CustomFieldTypeCheckbox, CustomFieldTypeSelect, CustomFieldTypeMultiSelect,
		CustomFieldTypeUser, CustomFieldTypeURL, CustomFieldTypeEmail, CustomFieldTypeRating,
		CustomFieldTypeProgress:
		return true
	}
	return false
}

func (t CustomFieldType) hasOptions() bool {
	return t == CustomFieldTypeSelect || t == CustomFieldTypeMultiSelect
}

// CustomFieldConfig holds the type specific settings of a custom field.
type CustomFieldConfig struct {
	Min       *float64 `json:"min,omitempty" doc:"Lowest allowed value. Applies to number and currency."`
	Max       *float64 `json:"max,omitempty" doc:"Highest allowed value. Applies to number and currency."`
	Precision *int     `json:"precision,omitempty" minimum:"0" maximum:"10" doc:"Number of decimal places clients should show. Applies to number and currency."`
	Currency  string   `json:"currency,omitempty" maxLength:"3" doc:"ISO 4217 currency code, e.g. TRY. Applies to currency."`
	MaxRating *int     `json:"max_rating,omitempty" minimum:"1" maximum:"10" doc:"Highest allowed rating, 5 when unset. Applies to rating."`
}

// CustomField is the definition of a field projects members can fill in on every task of a project.
type CustomField struct {
	ID        int64 `xorm:"bigint autoincr not null unique pk" json:"id" readOnly:"true" doc:"The unique, numeric id of this custom field."`
	ProjectID int64 `xorm:"bigint not null INDEX" json:"project_id" readOnly:"true" doc:"The project this field belongs to. Taken from the URL."`

	Title       string               `xorm:"varchar(250) not null" json:"title" valid:"required,runelength(1|250)" minLength:"1" maxLength:"250" doc:"The title of the field."`
	Description string               `xorm:"text null" json:"description" doc:"An optional description shown to people filling in the field."`
	Type        CustomFieldType      `xorm:"varchar(30) not null" json:"type" enum:"text,long_text,number,currency,date,checkbox,select,multi_select,user,url,email,rating,progress" doc:"The data type of the field. It cannot be changed after the field was created."`
	Required    bool                 `xorm:"bool default false" json:"required" doc:"Whether the field must be filled in. Not enforced on every save; workflow transitions use it later."`
	Config      *CustomFieldConfig   `xorm:"json null" json:"config" doc:"Type specific settings, e.g. min/max for number, max_rating for rating."`
	Position    float64              `xorm:"double not null" json:"position" doc:"The position of the field inside the project. New fields go last when it is 0."`
	Options     []*CustomFieldOption `xorm:"-" json:"options" doc:"The choices of a select or multi_select field. On update the submitted list replaces the stored one: entries with an id are updated, entries without one are created and missing ones are deleted together with every value that uses them."`
	CreatedByID int64                `xorm:"bigint not null" json:"-"`
	CreatedBy   *user.User           `xorm:"-" json:"created_by" readOnly:"true" doc:"The user who created the field."`
	Created     time.Time            `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this field was created. You cannot change this value."`
	Updated     time.Time            `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this field was last updated. You cannot change this value."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName returns the table name of custom fields.
func (*CustomField) TableName() string {
	return "custom_fields"
}

// CustomFieldOption is one choice of a select or multi_select field.
type CustomFieldOption struct {
	ID            int64   `xorm:"bigint autoincr not null unique pk" json:"id" doc:"The unique, numeric id of this option. Omit it to create a new option."`
	CustomFieldID int64   `xorm:"bigint not null INDEX" json:"custom_field_id" readOnly:"true" doc:"The field this option belongs to."`
	Title         string  `xorm:"varchar(250) not null" json:"title" minLength:"1" maxLength:"250" doc:"The title of the option."`
	Color         string  `xorm:"varchar(7) null" json:"color" maxLength:"7" doc:"Optional hex color including the leading #, e.g. #ff8800."`
	Position      float64 `xorm:"double not null" json:"position" doc:"The position of the option inside the field. Options go in the submitted order when it is 0."`
}

// TableName returns the table name of custom field options.
func (*CustomFieldOption) TableName() string {
	return "custom_field_options"
}

func getCustomFieldInProject(s *xorm.Session, fieldID, projectID int64) (*CustomField, error) {
	field := &CustomField{}
	exists, err := s.Where("id = ? AND project_id = ?", fieldID, projectID).Get(field)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCustomFieldDoesNotExist{CustomFieldID: fieldID}
	}
	return field, nil
}

func getCustomFieldOptions(s *xorm.Session, fieldIDs []int64) (map[int64][]*CustomFieldOption, error) {
	result := make(map[int64][]*CustomFieldOption, len(fieldIDs))
	if len(fieldIDs) == 0 {
		return result, nil
	}
	options := []*CustomFieldOption{}
	err := s.Where(builder.In("custom_field_id", fieldIDs)).
		OrderBy("position ASC, id ASC").
		Find(&options)
	if err != nil {
		return nil, err
	}
	for _, o := range options {
		result[o.CustomFieldID] = append(result[o.CustomFieldID], o)
	}
	return result, nil
}

func loadCustomFieldRelations(s *xorm.Session, fields ...*CustomField) error {
	ids := make([]int64, 0, len(fields))
	userIDs := make([]int64, 0, len(fields))
	for _, f := range fields {
		ids = append(ids, f.ID)
		userIDs = append(userIDs, f.CreatedByID)
	}
	options, err := getCustomFieldOptions(s, ids)
	if err != nil {
		return err
	}
	users, err := user.GetUsersByIDs(s, userIDs)
	if err != nil {
		return err
	}
	for _, f := range fields {
		f.Options = options[f.ID]
		if f.Options == nil {
			f.Options = []*CustomFieldOption{}
		}
		f.CreatedBy = users[f.CreatedByID]
	}
	return nil
}

func nextCustomFieldPosition(s *xorm.Session, projectID int64) (float64, error) {
	last := &CustomField{}
	exists, err := s.Where("project_id = ?", projectID).OrderBy("position DESC").Get(last)
	if err != nil {
		return 0, err
	}
	if !exists {
		return customFieldPositionStep, nil
	}
	return last.Position + customFieldPositionStep, nil
}

func (cf *CustomField) validate() error {
	cf.Title = strings.TrimSpace(cf.Title)
	if cf.Title == "" {
		return ErrCustomFieldInvalidDefinition{Reason: "title must not be empty"}
	}
	if len([]rune(cf.Title)) > 250 {
		return ErrCustomFieldInvalidDefinition{Reason: "title must not be longer than 250 characters"}
	}
	if !cf.Type.isValid() {
		return ErrCustomFieldInvalidDefinition{Reason: "unknown type " + string(cf.Type)}
	}

	if !cf.Type.hasOptions() && len(cf.Options) > 0 {
		return ErrCustomFieldInvalidDefinition{Reason: "only select and multi_select fields have options"}
	}
	for _, o := range cf.Options {
		o.Title = strings.TrimSpace(o.Title)
		if o.Title == "" || len([]rune(o.Title)) > 250 {
			return ErrCustomFieldInvalidDefinition{Reason: "option titles must be between 1 and 250 characters"}
		}
		if o.Color != "" && !customFieldColorRegex.MatchString(o.Color) {
			return ErrCustomFieldInvalidDefinition{Reason: "option colors must look like #aabbcc"}
		}
	}

	return cf.validateConfig()
}

func (cf *CustomField) validateConfig() error {
	c := cf.Config
	if c == nil {
		return nil
	}
	if c.Min != nil && (math.IsNaN(*c.Min) || math.IsInf(*c.Min, 0)) ||
		c.Max != nil && (math.IsNaN(*c.Max) || math.IsInf(*c.Max, 0)) {
		return ErrCustomFieldInvalidDefinition{Reason: "min and max must be finite numbers"}
	}
	if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
		return ErrCustomFieldInvalidDefinition{Reason: "min must not be greater than max"}
	}
	if c.Precision != nil && (*c.Precision < 0 || *c.Precision > maxCustomFieldPrecision) {
		return ErrCustomFieldInvalidDefinition{Reason: "precision must be between 0 and 10"}
	}
	if c.MaxRating != nil && (*c.MaxRating < 1 || *c.MaxRating > maxCustomFieldMaxRating) {
		return ErrCustomFieldInvalidDefinition{Reason: "max_rating must be between 1 and 10"}
	}
	if c.Currency != "" && len(c.Currency) != 3 {
		return ErrCustomFieldInvalidDefinition{Reason: "currency must be a three letter code"}
	}
	return nil
}

// Create creates a custom field with its options in a project.
// @Summary Create a custom field
func (cf *CustomField) Create(s *xorm.Session, a web.Auth) (err error) {
	cf.ID = 0
	if err = cf.validate(); err != nil {
		return err
	}

	// A link share's id is not a user id.
	cf.CreatedByID = 0
	if _, isShare := a.(*LinkSharing); !isShare {
		cf.CreatedByID = a.GetID()
	}
	if cf.Position == 0 {
		if cf.Position, err = nextCustomFieldPosition(s, cf.ProjectID); err != nil {
			return err
		}
	}

	if _, err = s.Insert(cf); err != nil {
		return err
	}

	for i, o := range cf.Options {
		o.ID = 0
		o.CustomFieldID = cf.ID
		if o.Position == 0 {
			o.Position = float64(i+1) * customFieldPositionStep
		}
		if _, err = s.Insert(o); err != nil {
			return err
		}
	}

	if err = loadCustomFieldRelations(s, cf); err != nil {
		return err
	}
	events.DispatchOnCommit(s, &CustomFieldCreatedEvent{
		CustomField: cf,
		Doer:        doerFromAuth(s, a),
	})
	return nil
}

// ReadOne adds the options and creator; CanRead already loaded the field itself.
// @Summary Get a custom field
func (cf *CustomField) ReadOne(s *xorm.Session, _ web.Auth) error {
	return loadCustomFieldRelations(s, cf)
}

// ReadAll returns the custom fields of a project ordered by position.
// @Summary List the custom fields of a project
func (cf *CustomField) ReadAll(s *xorm.Session, a web.Auth, search string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	// DoReadAll skips the Can* checks.
	if cf.ProjectID < 1 {
		return nil, 0, 0, ErrProjectDoesNotExist{ID: cf.ProjectID}
	}
	can, _, err := (&Project{ID: cf.ProjectID}).CanRead(s, a)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	var cond builder.Cond = builder.Eq{"project_id": cf.ProjectID}
	if search != "" {
		cond = cond.And(db.ILIKE("title", search))
	}

	total, err := s.Where(cond).Count(&CustomField{})
	if err != nil {
		return nil, 0, 0, err
	}

	fields := []*CustomField{}
	limit, start := getLimitFromPageIndex(page, perPage)
	q := s.Where(cond).OrderBy("position ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit, start)
	}
	if err = q.Find(&fields); err != nil {
		return nil, 0, 0, err
	}
	if err = loadCustomFieldRelations(s, fields...); err != nil {
		return nil, 0, 0, err
	}
	return fields, len(fields), total, nil
}

// Update updates a custom field and replaces its options by the submitted list.
// @Summary Update a custom field
func (cf *CustomField) Update(s *xorm.Session, a web.Auth) (err error) {
	existing, err := getCustomFieldInProject(s, cf.ID, cf.ProjectID)
	if err != nil {
		return err
	}
	if cf.Type == "" {
		cf.Type = existing.Type
	}
	if cf.Type != existing.Type {
		return ErrCustomFieldTypeImmutable{CustomFieldID: cf.ID, Type: existing.Type}
	}
	if err = cf.validate(); err != nil {
		return err
	}
	if cf.Position == 0 {
		cf.Position = existing.Position
	}

	_, err = s.ID(cf.ID).
		Cols("title", "description", "required", "config", "position").
		Update(cf)
	if err != nil {
		return err
	}

	if err = cf.replaceOptions(s); err != nil {
		return err
	}

	// reload: Update wrote only the editable columns
	updated, err := getCustomFieldInProject(s, cf.ID, cf.ProjectID)
	if err != nil {
		return err
	}
	options := cf.Options
	*cf = *updated
	cf.Options = options
	if err = loadCustomFieldRelations(s, cf); err != nil {
		return err
	}
	events.DispatchOnCommit(s, &CustomFieldUpdatedEvent{
		CustomField: cf,
		Doer:        doerFromAuth(s, a),
	})
	return nil
}

func (cf *CustomField) replaceOptions(s *xorm.Session) error {
	stored, err := getCustomFieldOptions(s, []int64{cf.ID})
	if err != nil {
		return err
	}
	byID := make(map[int64]*CustomFieldOption, len(stored[cf.ID]))
	for _, o := range stored[cf.ID] {
		byID[o.ID] = o
	}

	kept := make(map[int64]bool, len(cf.Options))
	for i, o := range cf.Options {
		if o.Position == 0 {
			o.Position = float64(i+1) * customFieldPositionStep
		}
		o.CustomFieldID = cf.ID

		if o.ID == 0 {
			if _, err = s.Insert(o); err != nil {
				return err
			}
			kept[o.ID] = true
			continue
		}

		if _, exists := byID[o.ID]; !exists {
			return ErrCustomFieldOptionDoesNotExist{CustomFieldID: cf.ID, OptionID: o.ID}
		}
		kept[o.ID] = true
		_, err = s.ID(o.ID).Cols("title", "color", "position").Update(o)
		if err != nil {
			return err
		}
	}

	removed := []int64{}
	for id := range byID {
		if !kept[id] {
			removed = append(removed, id)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	_, err = s.Where(builder.And(
		builder.Eq{"custom_field_id": cf.ID},
		builder.In("option_id", removed),
	)).Delete(&TaskCustomFieldValue{})
	if err != nil {
		return err
	}
	_, err = s.Where(builder.In("id", removed)).Delete(&CustomFieldOption{})
	return err
}

// Delete deletes a custom field together with its options and all values.
// @Summary Delete a custom field
func (cf *CustomField) Delete(s *xorm.Session, a web.Auth) (err error) {
	existing, err := getCustomFieldInProject(s, cf.ID, cf.ProjectID)
	if err != nil {
		return err
	}
	if err = loadCustomFieldRelations(s, existing); err != nil {
		return err
	}
	if err = deleteCustomFieldsByCond(s, builder.Eq{"id": existing.ID}); err != nil {
		return err
	}

	events.DispatchOnCommit(s, &CustomFieldDeletedEvent{
		CustomField: existing,
		Doer:        doerFromAuth(s, a),
	})
	return nil
}

// deleteCustomFieldsByCond removes the matching definitions with their options and values.
func deleteCustomFieldsByCond(s *xorm.Session, cond builder.Cond) error {
	ids := []int64{}
	if err := s.Table("custom_fields").Where(cond).Cols("id").Find(&ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if err := removeCustomFieldFromWorkflows(s, ids); err != nil {
		return err
	}
	if err := disableAutomationRulesForCustomFields(s, ids); err != nil {
		return err
	}
	if err := disableFormsForCustomFields(s, ids); err != nil {
		return err
	}
	if _, err := s.Where(builder.In("custom_field_id", ids)).Delete(&TaskCustomFieldValue{}); err != nil {
		return err
	}
	if _, err := s.Where(builder.In("custom_field_id", ids)).Delete(&CustomFieldOption{}); err != nil {
		return err
	}
	_, err := s.Where(builder.In("id", ids)).Delete(&CustomField{})
	return err
}

func deleteCustomFieldsForProject(s *xorm.Session, projectID int64) error {
	return deleteCustomFieldsByCond(s, builder.Eq{"project_id": projectID})
}

func deleteCustomFieldValuesForTask(s *xorm.Session, taskID int64) error {
	_, err := s.Where("task_id = ?", taskID).Delete(&TaskCustomFieldValue{})
	return err
}
