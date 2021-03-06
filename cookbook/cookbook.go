/* Cookbooks! The ultimate building block of any chef run. */

/*
 * Copyright (c) 2013-2014, Jeremy Bingham (<jbingham@gmail.com>)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package cookbook handles the basic building block of any chef (or goiardi)
// run, the humble cookbook.
package cookbook

import (
	"github.com/ctdk/goiardi/config"
	"github.com/ctdk/goiardi/data_store"
	"github.com/ctdk/goiardi/filestore"
	"github.com/ctdk/goiardi/util"
	"fmt"
	"strings"
	"strconv"
	"sort"
	"git.tideland.biz/goas/logger"
	"net/http"
	"regexp"
	"database/sql"
)

// Make version strings with the format "x.y.z" sortable.
type VersionStrings []string

// The Cookbook struct holds an array of cookbook versions, which is where the
// run lists, definitions, attributes, etc. are.
type Cookbook struct {
	Name string
	Versions map[string]*CookbookVersion
	latest *CookbookVersion
	numVersions *int
	id int32
}

/* We... want the JSON tags for this. */

// CookbookVersion is the meat of the cookbook. This is what's set when a new
// cookbook is uploaded.
type CookbookVersion struct {
	CookbookName string `json:"cookbook_name"`
	Name string `json:"name"`
	Version string `json:"version"`
	ChefType string `json:"chef_type"`
	JsonClass string `json:"json_class"`
	Definitions []map[string]interface{} `json:"definitions"`
	Libraries []map[string]interface{} `json:"libraries"`
	Attributes []map[string]interface{} `json:"attributes"`
	Recipes []map[string]interface{} `json:"recipes"`
	Providers []map[string]interface{} `json:"providers"`
	Resources []map[string]interface{} `json:"resources"`
	Templates []map[string]interface{} `json:"templates"`
	RootFiles []map[string]interface{} `json:"root_files"`
	Files []map[string]interface{} `json:"files"`
	IsFrozen bool `json:"frozen?"`
	Metadata map[string]interface{} `json:"metadata"` 
	id int32
	cookbook_id int32
}

/* Cookbook methods and functions */

func (c *Cookbook) GetName() string {
	return c.Name
}

func (c *Cookbook) URLType() string {
	return "cookbooks"
}

func (c *CookbookVersion) GetName() string {
	return c.Name
}

func (c *CookbookVersion) URLType() string {
	return "cookbooks"
}

// Create a new cookbook.
func New(name string) (*Cookbook, util.Gerror){
	var found bool
	if !util.ValidateEnvName(name) {
		err := util.Errorf("Invalid cookbook name '%s' using regex: 'Malformed cookbook name. Must only contain A-Z, a-z, 0-9, _ or -'.", name)
		return nil, err
	}
	if config.Config.UseMySQL {
		var cerr error
		found, cerr = checkForCookbookMySQL(data_store.Dbh, name)
		if cerr != nil {
			err := util.CastErr(cerr)
			err.SetStatus(http.StatusInternalServerError)
			return nil, err
		} 
	} else {
		ds := data_store.New()
		_, found = ds.Get("cookbook", name)
	}
	if found {
		err := util.Errorf("Cookbook %s already exists", name)
		err.SetStatus(http.StatusConflict)
	}
	cookbook := &Cookbook{
		Name: name,
		Versions: make(map[string]*CookbookVersion),
	}
	return cookbook, nil
}

// The number of versions this cookbook has.
func (c *Cookbook)NumVersions() int {
	if config.Config.UseMySQL {
		if c.numVersions == nil {
			c.numVersions = c.numVersionsMySQL()
		}
		return *c.numVersions
	} else {
		return len(c.Versions)
	}
}

// Return all the cookbooks that have been uploaded to this server.
func AllCookbooks() (cookbooks []*Cookbook) {
	if config.Config.UseMySQL {
		cookbooks = allCookbooksMySQL()
	} else {
		cookbook_list := GetList()
		for _, c := range cookbook_list {
			cb, err := Get(c)
			if err != nil {
				logger.Debugf("Curious. Cookbook %s was in the cookbook list, but wasn't found when fetched. Continuing.", c)
				continue
			}
			cookbooks = append(cookbooks, cb)
		}
	}
	return cookbooks
}

// Get a cookbook.
func Get(name string) (*Cookbook, util.Gerror){
	var cookbook *Cookbook
	var found bool
	if config.Config.UseMySQL {
		var err error
		cookbook, err = getCookbookMySQL(name)
		if err != nil {
			if err == sql.ErrNoRows {
				found = false
			} else {
				gerr := util.CastErr(err)
				gerr.SetStatus(http.StatusInternalServerError)
				return nil, gerr
			}
		} else {
			found = true
		}
	} else {
		ds := data_store.New()
		var c interface{}
		c, found = ds.Get("cookbook", name)
		if c != nil {
			cookbook = c.(*Cookbook)
		}
	}
	if !found {
		err := util.Errorf("Cannot find a cookbook named %s", name)
		err.SetStatus(http.StatusNotFound)
		return nil, err
	}
	return cookbook, nil
}

// Save a cookbook to the in-memory data store or database.
func (c *Cookbook) Save() error {
	if config.Config.UseMySQL {
		return c.saveCookbookMySQL()
	} else {
		ds := data_store.New()
		ds.Set("cookbook", c.Name, c)
	}
	return nil
}

func (c *Cookbook) Delete() error {
	if config.Config.UseMySQL {
		return c.deleteCookbookMySQL()
	} else {
		ds := data_store.New()
		ds.Delete("cookbook", c.Name)
	}
	return nil
}

// Get a list of all cookbooks on this server.
func GetList() []string {
	if config.Config.UseMySQL {
		return getCookbookListMySQL()
	} 
	ds := data_store.New()
	cb_list := ds.GetList("cookbook")
	return cb_list
}

/* Returns a sorted list of all the versions of this cookbook */
func (c *Cookbook)sortedVersions() ([]*CookbookVersion){
	if config.Config.UseMySQL {
		return c.sortedCookbookVersionsMySQL()
	} 
	sorted := make([]*CookbookVersion, len(c.Versions))
	keys := make(VersionStrings, len(c.Versions))

	u := 0
	for k, _ := range c.Versions {
		keys[u] = k
		u++
	}
	sort.Sort(sort.Reverse(keys))

	/* populate sorted now */
	for i, s := range keys {
		/* This shouldn't be able to happen, but somehow it... does? */
		if i >= len(sorted) {
			break
		}
		sorted[i] = c.Versions[s]
	}
	return sorted
}

// Update what the cookbook stores as the latest version available.
func (c *Cookbook) UpdateLatestVersion() {
	c.latest = nil
	c.LatestVersion()
}

// Get the latest version of this cookbook.
func (c *Cookbook) LatestVersion() *CookbookVersion {
	if c.latest == nil {
		sorted := c.sortedVersions()
		c.latest = sorted[0]
	}
	return c.latest
}

// Gets num_results (or all if num_results is nil) versions of a cookbook,
// returning a hash describing the cookbook and the versions returned.
func (c *Cookbook)InfoHash(num_results interface{}) map[string]interface{} {
	return c.infoHashBase(num_results, "")
}

// Gets num_results (or all if num_results is nil) versions of a cookbook that
// match the given constraint and returns a hash describing the cookbook and the
// versions returned.
func (c *Cookbook)ConstrainedInfoHash(num_results interface{}, constraint string) map[string]interface{} {
	return c.infoHashBase(num_results, constraint)
}

// For the given run list and environment constraints, return the cookbook
// dependencies.
func DependsCookbooks(run_list []string, env_constraints map[string]string) (map[string]interface{}, error) {
	cd_list := make(map[string][]string, len(run_list))
	run_list_ref := make([]string, len(run_list))

	for i, cb_v := range run_list {
		var cbName string
		var constraint string
		cx := strings.Split(cb_v, "@")
		cbName = strings.Split(cx[0], "::")[0]
		if len(cx) == 2 {
			constraint = fmt.Sprintf("= %s", cx[1])
		}
		cd_list[cbName] = []string{constraint}
		/* There's a method to our madness. We need to modify the
		 * cd_list as we go along, but want the base list to remain the
		 * same. Thus, we make an additional array of cookbook names to
		 * range through. */
		run_list_ref[i] = cbName
	}

 	for k, ec := range env_constraints {
 		if _, found := cd_list[k]; !found {
 			continue
 		} else {
			/* Check if there's a constraint in the uploaded run
			 * list. If not take the env one and move on. */
			if cd_list[k][0] == "" {
				cd_list[k] = []string{ ec }
				continue
			}
 			/* Override if the environment is more restrictive */
			_, orgver, _ := splitConstraint(cd_list[k][0])
			newop, newver, nerr := splitConstraint(ec)
			if nerr != nil {
				return nil, nerr
			}
			/* if the versions are equal, take the env one */
			if orgver == newver {
				cd_list[k] = []string{ ec }
				continue /* and break out of this step */
			}
			switch newop {
				case ">", ">=":
					if versionLess(orgver, newver) {
						cd_list[k] = []string{ ec }
					} 
				case "<", "<=":
					if !versionLess(orgver, newver) {
						cd_list[k] = []string{ ec }
					}
				case "=":
					if orgver != newver {
						err := fmt.Errorf("This run list has a constraint '%s' for %s that conflicts with '%s' in the environment's cookbook versions.", cd_list[k][0], k, ec)
						return nil, err
					}
				case "~>":
					if action := verConstraintCheck(orgver, newver, newop); action == "ok" {
						cd_list[k] = []string{ ec }
					} else {
						err := fmt.Errorf("This run list has a constraint '%s' for %s that conflicts with '%s' in the environment's cookbook versions.", cd_list[k][0], k, ec)
						return nil, err
					}
				default:
					err := fmt.Errorf("An unlikely occurance, but the constraint '%s' for cookbook %s in this environment is impossible.", ec, k)
					return nil, err
			}
 		}
 	}

	/* Build a slice holding all the needed cookbooks. */
	for _, cbName := range run_list_ref {
		c, err := Get(cbName)
		if err != nil {
			return nil, err
		}
		cbv := c.LatestConstrained(cd_list[cbName][0])
		if cbv == nil {
			return nil, fmt.Errorf("No cookbook found for %s that satisfies constraint '%s'", c.Name, cd_list[cbName][0])
		}
		
		nerr := cbv.resolveDependencies(cd_list)
		if nerr != nil {
			return nil, nerr
		}
	}

	cookbook_deps := make(map[string]interface{}, len(cd_list))
	for cname, traints := range cd_list {
		cb, err := Get(cname)
		/* Although we would have already seen this, but being careful
		 * rarely hurt. */
		if err != nil {
			return nil, err
		}
		var gcbv *CookbookVersion

		for _, cv := range cb.sortedVersions(){
			Vers:
			for _, ct := range traints {
				if ct != "" { // no constraint
					op, ver, err := splitConstraint(ct)
					if err != nil {
						return nil, err
					}
					if action := verConstraintCheck(cv.Version, ver, op); action != "ok" {
						// BREAK THIS LOOP, BUT CONTINUE THE cv LOOP. HMM
						continue Vers
					}
				}
			}
			/* If we pass the constraint tests, set gcbv to cv and
			 * break. */
			gcbv = cv
			break
		}
		if gcbv == nil {
			err := fmt.Errorf("Unfortunately no version of %s could satisfy the requested constraints: %s", cname, strings.Join(traints, ", "))
			return nil, err
		} else {
			gcbvJson := gcbv.ToJson("POST")
			/* Sigh. For some reason, *some* places want nothing
			 * sent for cookbook information divisions like 
			 * attributes, libraries, providers, etc. However, 
			 * others will flip out if nothing is sent at all, and
			 * environment/<env>/cookbook_versions is one of them.
			 * Go through the list of possibly guilty divisions and
			 * set them to an empty slice of maps if they're nil. */
			chkDiv := []string{ "definitions", "libraries", "attributes", "providers", "resources", "templates", "root_files", "files" }
			for _, cd := range chkDiv {
				if gcbvJson[cd] == nil {
					gcbvJson[cd] = make([]map[string]interface{}, 0)
				}
			}
			cookbook_deps[gcbv.CookbookName] = gcbvJson
		}
	}

	return cookbook_deps, nil
}

func (cbv *CookbookVersion)resolveDependencies(cd_list map[string][]string) error {
	dep_list := cbv.Metadata["dependencies"].(map[string]interface{})

	for r, c2 := range dep_list {
		c := c2.(string)
		dep_cb, err := Get(r)
		if err != nil {
			return err
		}
		deb_cbv := dep_cb.LatestConstrained(c)
		if deb_cbv == nil {
			err := fmt.Errorf("No cookbook version for %s satisfies constraint '%s'.", r, c)
			return err
		}

		/* Do we satisfy the constraints we have? */
		if constraints, found := cd_list[r]; found {
			for _, dcon := range constraints {
				if dcon != "" {
					op, ver, err := splitConstraint(dcon)
					if err != nil {
						return err
					}
					stat := verConstraintCheck(deb_cbv.Version, ver, op)
					if stat != "ok" {
						err := fmt.Errorf("Oh no! Cookbook %s (ver %s) depends on a version of cookbook %s matching the constraint '%s', but that constraint conflicts with the previous constraint of '%s'. Bailing, sorry.", cbv.CookbookName, cbv.Version, deb_cbv.CookbookName, c, dcon)
						return err
					}
				}
			}
		} else {
			/* Add our constraint */
			cd_list[r] = []string{c}
		}
		
		nerr := deb_cbv.resolveDependencies(cd_list)
		if nerr != nil {
			return nerr
		}
	}
	return nil
}

func splitConstraint(constraint string) (string, string, error) {
	t1 := strings.Split(constraint, " ")
	if len(t1) != 2 {
		err := fmt.Errorf("Constraint '%s' was not well-formed.", constraint)
		return "", "", err
	} else {
		op := t1[0]
		ver := t1[1]
		return op, ver, nil
	}
}

func (c *Cookbook)infoHashBase(num_results interface{}, constraint string) map[string]interface{} {
	cb_hash := make(map[string]interface{})
	cb_hash["url"] = util.ObjURL(c)
	
	nr := 0
	
	/* Working to maintain Chef server behavior here. We need to make "all"
	 * give all versions of the cookbook and make no value give one version,
	 * but keep 0 as invalid input that gives zero results back. This might
	 * be an area worth breaking. */
	var num_versions int
	all_versions := false
	//var cb_hash_len int

	if num_results != "" && num_results != "all" {
		num_versions, _ = strconv.Atoi(num_results.(string))
	} else if num_results == "" {
		num_versions = 1
	} else {
		all_versions = true
	}

	cb_hash["versions"] = make([]interface{}, 0)

	var constraint_version string
	var constraint_op string
	if constraint != "" {
		traints := strings.Split(constraint, " ")
		/* If the constraint isn't well formed like ">= 1.2.3", log the
		 * fact and ignore the constraint. */
		if len(traints) == 2 {
			constraint_version = traints[1]
			constraint_op = traints[0]
		} else {
			logger.Warningf("Constraint '%s' for cookbook %s was badly formed -- bailing.\n", constraint, c.Name)
			return nil
		}
	}

	VerLoop:
	for _, cv := range c.sortedVersions() {
		if !all_versions && nr >= num_versions {
			break
		} 
		/* Version constraint checking. */
		if constraint != "" {
			con_action := verConstraintCheck(cv.Version, constraint_version, constraint_op)
			switch con_action {
				case "skip":
					/* Skip this version, keep going. */
					continue VerLoop
				case "break":
					/* Stop processing entirely. */
					break VerLoop
				/* Default action is, of course, to continue on
				 * like nothing happened. Later, we need to
				 * panic over an invalid constraint. */
			}
		}
		cv_info := make(map[string]string)
		cv_info["url"] = util.CustomObjURL(c, cv.Version)
		cv_info["version"] = cv.Version
		cb_hash["versions"] = append(cb_hash["versions"].([]interface{}), cv_info)
		nr++ 
	}
	return cb_hash
}

// Returns the latest version of a cookbook that matches the given constraint.
// If no constraint is given, returns the latest version.
func (c *Cookbook) LatestConstrained(constraint string) *CookbookVersion{
	if constraint == "" {
		return c.LatestVersion()
	}
	var constraint_version string
	var constraint_op string
	traints := strings.Split(constraint, " ")
	if len(traints) == 2 {
		constraint_version = traints[1]
		constraint_op = traints[0]
	} else {
		logger.Warningf("Constraint '%s' for cookbook %s (in LatestConstrained) was malformed. Bailing.\n", constraint, c.Name)
		return nil
	}
	for _, cv := range c.sortedVersions(){
		action := verConstraintCheck(cv.Version, constraint_version, constraint_op)
		/* We only want the latest that works. */
		if (action == "ok"){
			return cv
		}
	}
	/* if nothing satisfied the constraint, we have to return nil */
	return nil
}



/* CookbookVersion methods and functions */

// Create a new version of the cookbook.
func (c *Cookbook)NewVersion(cb_version string, cbv_data map[string]interface{}) (*CookbookVersion, util.Gerror){
	if _, err := c.GetVersion(cb_version); err == nil {
		err := util.Errorf("Version %s of cookbook %s already exists, and shouldn't be created like this. Use UpdateVersion instead.", cb_version, c.Name)
		err.SetStatus(http.StatusConflict)
		return nil, err
	}
	cbv := &CookbookVersion{
		CookbookName: c.Name,
		Version: cb_version,
		Name: fmt.Sprintf("%s-%s", c.Name, cb_version),
		ChefType: "cookbook_version",
		JsonClass: "Chef::CookbookVersion",
		IsFrozen: false,
		cookbook_id: c.id, // should be ok even with in-mem
	}
	err := cbv.UpdateVersion(cbv_data, "")
	if err != nil {
		return nil, err
	}
	/* And, dur, add it to the versions */
	c.Versions[cb_version] = cbv
	
	c.numVersions = nil
	c.UpdateLatestVersion()
	c.Save()
	return cbv, nil
}



// Get a particular version of the cookbook.
func (c *Cookbook)GetVersion(cbVersion string) (*CookbookVersion, util.Gerror) {
	if cbVersion == "_latest" {
		return c.LatestVersion(), nil
	}
	var cbv *CookbookVersion
	var found bool

	if config.Config.UseMySQL {
		// Ridiculously cacheable, but let's get it working first. This
		// applies all over the place w/ the SQL bits.
		if cbv, found = c.Versions[cbVersion]; !found {
			var err error
			cbv, err = c.getCookbookVersionMySQL(cbVersion)
			if err != nil {
				if err == sql.ErrNoRows {
					found = false
				} else {
					gerr := util.Errorf(err.Error())
					gerr.SetStatus(http.StatusInternalServerError)
					return nil, gerr
				}
			} else {
				found = true
				c.Versions[cbVersion] = cbv
			}
		}
	} else {
		cbv, found = c.Versions[cbVersion]
	}

	if !found {
		err := util.Errorf("Cannot find a cookbook named %s with version %s", c.Name, cbVersion)
		err.SetStatus(http.StatusNotFound)
		return nil, err
	}
	return cbv, nil
}

func extractVerNums(cbVersion string) (maj, min, patch int64, err util.Gerror) {
	if _, err = util.ValidateAsVersion(cbVersion); err != nil {
		return 0, 0, 0, err
	}
	nums := strings.Split(cbVersion, ".")
	if len(nums) < 2 && len(nums) > 3 {
		err = util.Errorf("incorrect number of numbers in version string '%s'", len(nums))
		return 0, 0, 0, err
	}
	var vt int64
	var nerr error
	vt, nerr = strconv.ParseInt(nums[0], 0, 64)
	if nerr != nil {
		err = util.Errorf(nerr.Error())
		return 0, 0, 0, err
	}
	maj = vt
	vt, nerr = strconv.ParseInt(nums[1], 0, 64)
	if nerr != nil {
		err = util.Errorf(nerr.Error())
		return 0, 0, 0, err
	}
	min = vt
	if len(nums) == 3 {
		vt, nerr = strconv.ParseInt(nums[2], 0, 64)
		if nerr != nil {
			err = util.Errorf(nerr.Error())
			return 0, 0, 0, err
		}
		patch = vt
	} else {
		patch = 0
	}
	return maj, min, patch, nil
}

func (c *Cookbook)deleteHashes(file_hashes []string) {
	/* And remove the unused hashes. Currently, sigh, this involes checking
	 * every cookbook. Probably will be easier with an actual database, I
	 * imagine. */
	all_cookbooks := AllCookbooks()
	for _, cb := range all_cookbooks {
		/* just move on if we don't find it somehow */
		for _, ver := range cb.sortedVersions() {
			ver_hash := ver.fileHashes()
			for _, vh := range ver_hash {
				for i, fh := range file_hashes {
					/* If a hash in a deleted cookbook is
					 * in another cookbook, remove it from
					 * the hash to delete. Then we can break
					 * out. If we find that the hash we're
					 * comparing with is greater than this
					 * one in file_hashes, also break out.
					 */
					if fh == vh {
						file_hashes = delSliceElement(i, file_hashes)
						break
					} else if fh > vh {
						break
					}
				}
			}
		}
	}
	/* And delete whatever file hashes we still have */
	filestore.DeleteHashes(file_hashes)
}

// Delete a particular version of a cookbook.
func (c *Cookbook)DeleteVersion(cb_version string) util.Gerror {
	/* Check for existence */
	cbv, _ := c.GetVersion(cb_version)
	if cbv == nil {
		err := util.Errorf("Version %s of cookbook %s does not exist to be deleted.", cb_version, c.Name)
		err.SetStatus(http.StatusNotFound)
		return err
	} 

	file_hashes := cbv.fileHashes()

	if config.Config.UseMySQL {
		err := cbv.deleteCookbookVersionMySQL()
		if err != nil {
			return nil
		}
	}
	c.numVersions = nil

	delete(c.Versions, cb_version)
	c.deleteHashes(file_hashes)
	
	c.Save()
	return nil
}

// Update a specific version of a cookbook.
func (cbv *CookbookVersion)UpdateVersion(cbv_data map[string]interface{}, force string) util.Gerror {
	/* Allow force to update a frozen cookbook */
	if cbv.IsFrozen == true && force != "true" {
		err := util.Errorf("The cookbook %s at version %s is frozen. Use the 'force' option to override.", cbv.CookbookName, cbv.Version)
		err.SetStatus(http.StatusConflict)
		return err
	}

	file_hashes := cbv.fileHashes()
	
	_, nerr := util.ValidateAsString(cbv_data["cookbook_name"])
	if nerr != nil {
		if nerr.Error() == "Field 'name' missing" {
			nerr = util.Errorf("Field 'cookbook_name' missing")
		} else {
			nerr = util.Errorf("Field 'cookbook_name' invalid")
		}
		return nerr
	}

	/* Validation, validation, all is validation. */
	valid_elements := []string{ "cookbook_name", "name", "version", "json_class", "chef_type", "definitions", "libraries", "attributes", "recipes", "providers", "resources", "templates", "root_files", "files", "frozen?", "metadata", "force" }
	ValidElem:
	for k, _ := range cbv_data {
		for _, i := range valid_elements {
			if k == i {
				continue ValidElem
			}
		}
		err := util.Errorf("Invalid key %s in request body", k)
		return err
	}

	var verr util.Gerror
	cbv_data["chef_type"], verr = util.ValidateAsFieldString(cbv_data["chef_type"])
	if verr != nil {
		if verr.Error() == "Field 'name' nil" {
			cbv_data["chef_type"] = cbv.ChefType
		} else {
			verr = util.Errorf("Field 'chef_type' invalid")
			return verr
		}
	} else {
		// Wait, what was I doing here?
		// if !util.ValidateEnvName(cbv_data["chef_type"].(string)) {
		if cbv_data["chef_type"].(string) != "cookbook_version" {
			verr = util.Errorf("Field 'chef_type' invalid")
			return verr
		}
	}

	cbv_data["json_class"], verr = util.ValidateAsFieldString(cbv_data["json_class"])
	if verr != nil {
		if verr.Error() == "Field 'name' nil" {
			cbv_data["json_class"] = cbv.JsonClass
		} else {
			verr = util.Errorf("Field 'json_class' invalid")
			return verr
		}
	} else {
		if cbv_data["json_class"].(string) != "Chef::CookbookVersion" {
			verr = util.Errorf("Field 'json_class' invalid")
			return verr
		}
	}

	cbv_data["version"], verr = util.ValidateAsVersion(cbv_data["version"])
	if verr != nil {
		verr = util.Errorf("Field 'version' invalid")
		return verr
	} else {
		if cbv_data["version"].(string) == "0.0.0" && cbv.Version != "" {
			cbv_data["version"] = cbv.Version
		}
	}

	divs := []string{ "definitions", "libraries", "attributes", "recipes", "providers", "resources", "templates", "root_files", "files" }
	for _, d := range divs {
		cbv_data[d], verr = util.ValidateCookbookDivision(d, cbv_data[d])
		if verr != nil {
			return verr
		}
	}
	cbv_data["metadata"], verr = util.ValidateCookbookMetadata(cbv_data["metadata"])
	if verr != nil {
		return verr
	}
	

	cbv_data["frozen?"], verr = util.ValidateAsBool(cbv_data["frozen?"])
	if verr != nil {
		return verr
	}

	/* Basic sanity checking */
	if cbv_data["cookbook_name"].(string) != cbv.CookbookName {
		err := util.Errorf("Field 'cookbook_name' invalid")
		return err
	}
	if cbv_data["name"].(string) != cbv.Name {
		err := util.Errorf("Field 'name' invalid")
		return err
	}
	if cbv_data["version"].(string) != cbv.Version && cbv_data["version"] != "0.0.0" {
		err := util.Errorf("Field 'version' invalid")
		return err
	}
	
	/* Update the data */
	/* With these next two, should we test for existence before setting? */
	cbv.ChefType = cbv_data["chef_type"].(string)
	cbv.JsonClass = cbv_data["json_class"].(string)
	cbv.Definitions = convertToCookbookDiv(cbv_data["definitions"])
	cbv.Libraries = convertToCookbookDiv(cbv_data["libraries"])
	cbv.Attributes = convertToCookbookDiv(cbv_data["attributes"])
	cbv.Recipes = cbv_data["recipes"].([]map[string]interface{})
	cbv.Providers = convertToCookbookDiv(cbv_data["providers"])
	cbv.Resources = convertToCookbookDiv(cbv_data["resources"])
	cbv.Templates = convertToCookbookDiv(cbv_data["templates"])
	cbv.RootFiles = convertToCookbookDiv(cbv_data["root_files"])
	cbv.Files = convertToCookbookDiv(cbv_data["files"])
	if cbv.IsFrozen != true {
		cbv.IsFrozen = cbv_data["frozen?"].(bool)
	}
	cbv.Metadata = cbv_data["metadata"].(map[string]interface{})

	/* If we're using SQL, update this version in the DB. */
	if config.Config.UseMySQL {
		if err := cbv.updateCookbookVersionMySQL(); err != nil {
			return err
		}
	}

	/* Clean cookbook hashes */
	if len(file_hashes) > 0 {
		// Get our parent. Bravely assuming that if it exists we exist.
		cbook, _ := Get(cbv.CookbookName)
		cbook.deleteHashes(file_hashes)
	}
	
	return nil
}

func convertToCookbookDiv(div interface{}) []map[string]interface{} {
	switch div := div.(type) {
		case []map[string]interface{}:
			return div
		default:
			return nil
	}
}

// Get the hashes of all files associated with a cookbook. Useful for comparing
// the files in a deleted cookbook version with the files in other versions to
// figure out which to remove and which to keep. 
func (cbv *CookbookVersion)fileHashes() []string{
	/* Hmm. Weird as it seems, we seem to want length to be zero here so
	 * we can happily append. Otherwise we'll end up with a nil element. */
	fhashes := make([]string, 0)
	fhashes = append(fhashes, getAttrHashes(cbv.Definitions)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Libraries)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Attributes)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Recipes)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Providers)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Resources)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Templates)...)
	fhashes = append(fhashes, getAttrHashes(cbv.RootFiles)...)
	fhashes = append(fhashes, getAttrHashes(cbv.Files)...)

	/* Sort, then remove any duplicates */
	sort.Strings(fhashes)
	fhashes = removeDupHashes(fhashes)

	return fhashes
}

// Helper function that coverts the internal representation of a cookbook
// version to JSON in a way that knife and chef-client expect.
func (cbv *CookbookVersion)ToJson(method string) map[string]interface{} {
	toJson := make(map[string]interface{})
	toJson["name"] = cbv.Name
	toJson["cookbook_name"] = cbv.CookbookName
	if cbv.Version != "0.0.0" {
		toJson["version"] = cbv.Version
	}
	toJson["chef_type"] = cbv.ChefType
	toJson["json_class"] = cbv.JsonClass
	toJson["frozen?"] = cbv.IsFrozen
	toJson["recipes"] = cbv.Recipes
	toJson["metadata"] = cbv.Metadata

	/* Only send the other fields if something exists in them */
	/* Seriously, though, why *not* send the URL for the resources back 
	 * with PUT, but *DO* send it with everything else? */
	if cbv.Providers != nil && len(cbv.Providers) != 0 {
		toJson["providers"] = methodize(method, cbv.Providers)
	}
	if cbv.Definitions != nil && len(cbv.Definitions) != 0 {
		toJson["definitions"] = methodize(method, cbv.Definitions)
	}
	if cbv.Libraries != nil && len(cbv.Libraries) != 0 {
		toJson["libraries"] = methodize(method, cbv.Libraries)
	}
	if cbv.Attributes != nil && len(cbv.Attributes) != 0 {
		toJson["attributes"] = methodize(method, cbv.Attributes)
	}
	if cbv.Resources != nil && len(cbv.Resources) != 0 {
		toJson["resources"] = methodize(method, cbv.Resources)
	}
	if cbv.Templates != nil && len(cbv.Templates) != 0 {
		toJson["templates"] = methodize(method, cbv.Templates)
	}
	if cbv.RootFiles != nil && len(cbv.RootFiles) != 0 {
		toJson["root_files"] = methodize(method, cbv.RootFiles)
	}
	if cbv.Files != nil && len(cbv.Files) != 0 {
		toJson["files"] = methodize(method, cbv.Files)
	}

	return toJson
}

func methodize(method string, cb_thing []map[string]interface{}) []map[string]interface{} {
	ret_hash := make([]map[string]interface{}, len(cb_thing))
	for i, v := range cb_thing {
		ret_hash[i] = make(map[string]interface{})
		for k, j := range v {
			if method == "PUT" && k == "url" {
				continue
			}
			ret_hash[i][k] = j
		}
	}
	return ret_hash
}

func getAttrHashes(attr []map[string]interface{}) []string {
	hashes := make([]string, len(attr))
	for i, v := range attr {
		/* Woo, type assertion again */
		switch h := v["checksum"].(type) {
			case string:
				hashes[i] = h
			case nil:
				/* anything special here? */
				;
			default:
				; // do we expect an err?
		}
	}
	return hashes
}

func removeDupHashes(file_hashes []string) []string{
	for i, v := range file_hashes {
		/* break if we're the last element */
		if i + 1 >= len(file_hashes){
			break
		}
		/* If the current element is equal to the next one, remove 
		 * the next element. */
		if v == file_hashes[i + 1] {
			file_hashes = delSliceElement(i + 1, file_hashes)
		}
	}
	return file_hashes
}

func delSliceElement(pos int, strs []string) []string {
	strs = append(strs[:pos], strs[pos+1:]...)
	return strs
}

// Provide a list of recipes in this cookbook version. 
func (cbv *CookbookVersion) RecipeList() ([]string, util.Gerror) {
	recipe_meta := cbv.Recipes
	recipes := make([]string, len(recipe_meta))
	ci := 0
	/* Cobble the recipes together from the Recipes field */
	for _, r := range recipe_meta {
		rm := regexp.MustCompile(`(.*?)\.rb`)
		rfind := rm.FindStringSubmatch(r["name"].(string))
		if rfind == nil {
			/* unlikely */
			err := util.Errorf("No recipe name found")
			return nil, err
		}
		rbase := rfind[1]
		var rname string
		if rbase == "default" {
			rname = cbv.CookbookName
		} else {
			rname = fmt.Sprintf("%s::%s", cbv.CookbookName, rbase)
		}
		recipes[ci] = rname
		ci++
	}
	return recipes, nil
}

/* Version string functions to implement sorting */

func (v VersionStrings) Len() int {
	return len(v)
}

func (v VersionStrings) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

func (v VersionStrings) Less(i, j int) bool {
	return versionLess(v[i], v[j])
}

func versionLess(ver_a, ver_b string) bool {
	/* Chef cookbook versions are always to be in the form x.y.z (with x.y
	 * also allowed. This simplifies things a bit. */

	/* Easy comparison. False if they're equal. */
	if ver_a == ver_b {
		return false
	}

	/* Would caching the split strings ever be particularly worth it? */
	i_ver := strings.Split(ver_a, ".")
	j_ver := strings.Split(ver_b, ".")

	for q := 0; q < 3; q++ {
		/* If one of them doesn't actually exist, then obviously the
		 * other is bigger, and we're done. Of course this should only
		 * happen with the 3rd element. */
		if len(i_ver) < q + 1 {
			return true
		} else if len(j_ver) < q + 1 {
			return false
		}

		ic := i_ver[q]
		jc := j_ver[q]

		/* Otherwise, see if they're equal. If they're not, return the
		 * result of x < y. */
		ici, _ := strconv.Atoi(ic)
		jci, _ := strconv.Atoi(jc)
		if ici != jci {
			return ici < jci
		}
	}
	return false
}

/* Compares a version number against a constraint, like version 1.2.3 vs. 
 * ">= 1.0.1". In this case, 1.2.3 passes. It would not satisfy "= 1.2.0" or
 * "< 1.0", though. */

func verConstraintCheck(ver_a, ver_b, op string) string {
	switch op {
		case "=":
			if ver_a == ver_b {
				return "ok"
			} else if versionLess(ver_a, ver_b) {
				/* If we want equality and ver_a is less than
				 * version b, since the version list is sorted
				 * in descending order we've missed our chance.
				 * So, break out. */
				return "break"
			} else {
				return "skip"
			}
		case ">":
			if ver_a == ver_b || versionLess(ver_a, ver_b) {
				return "break"
			} else {
				return "ok"
			}
		case "<":
			/* return skip here because we might find what we want
			 * later. */
			if ver_a == ver_b || !versionLess(ver_a, ver_b){
				return "skip"
			} else {
				return "ok"
			}
		case ">=":
			if !versionLess(ver_a, ver_b) {
				return "ok"
			} else {
				return "break"
			}
		case "<=":
			if ver_a == ver_b || versionLess(ver_a, ver_b) {
				return "ok"
			} else {
				return "skip"
			}
		case "~>":
			/* only check pessimistic constraints if they can
			 * possibly be valid. */
			if versionLess(ver_a, ver_b) {
				return "break"
			}
			var upper_bound string
			pv := strings.Split(ver_b, ".")
			if len(pv) == 3 {
				uver, _ := strconv.Atoi(pv[1])
				uver++
				upper_bound = fmt.Sprintf("%s.%d", pv[0], uver)
			} else {
				uver, _ := strconv.Atoi(pv[0])
				uver++
				upper_bound = fmt.Sprintf("%d.0", uver)
			}
			if !versionLess(ver_a, ver_b) && versionLess(ver_a, upper_bound) {

				return "ok"
			} else {
				return "skip"
			}
		default:
			return "invalid"
	}
}

