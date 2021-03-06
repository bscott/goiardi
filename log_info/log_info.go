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

/* Package log_info tracks changes to objects when they're saved, noting the actor performing the action, what kind of action it was, the time of the change, the type of object and its id, and a dump of the object's state. */
package log_info

import (
	"github.com/ctdk/goiardi/data_store"
	"github.com/ctdk/goiardi/actor"
	"github.com/ctdk/goiardi/config"
	"github.com/ctdk/goiardi/util"
	"fmt"
	"time"
	"reflect"
	"database/sql"
	"sort"
	"git.tideland.biz/goas/logger"
)

type LogInfo struct {
	Actor actor.Actor `json:"-"`
	ActorInfo string `json:"actor_info"`
	ActorType string `json:"actor_type"`
	Time time.Time `json:"time"`
	Action string `json:"action"`
	ObjectType string `json:"object_type"`
	ObjectName string `json:"object_name"`
	ExtendedInfo string `json:"extended_info"`
	Id int `json:"id"`
}

// Write an event of the action type, performed by the given actor, against the
// given object.
func LogEvent(doer actor.Actor, obj util.GoiardiObj, action string) error {
	if !config.Config.LogEvents {
		logger.Debugf("Not logging this event")
		return nil
	} else {
		logger.Debugf("Logging event")
	}
	var actor_type string
	if doer.IsUser() {
		actor_type = "user"
	} else {
		actor_type = "client"
	}
	le := new(LogInfo)
	le.Action = action
	le.Actor = doer
	le.ActorType = actor_type
	le.ObjectName = obj.GetName()
	le.ObjectType = reflect.TypeOf(obj).String()
	le.Time = time.Now()
	ext_info, err := data_store.EncodeToJSON(obj)
	if err != nil {
		return err
	}
	le.ExtendedInfo = ext_info
	actor_info, err := data_store.EncodeToJSON(doer)
	if err != nil {
		return err
	}
	le.ActorInfo = actor_info

	if config.Config.UseMySQL {
		return le.writeEventMySQL()
	} else {
		return le.writeEventInMem()
	}
}

func (le *LogInfo)writeEventInMem() error {
	ds := data_store.New()
	return ds.SetLogInfo(le)
}

// Get a particular event by its id.
func Get(id int) (*LogInfo, error) {
	var le *LogInfo

	if config.Config.UseMySQL {
		var err error
		le, err = getLogEventMySQL(id)
		if err != nil {
			if err == sql.ErrNoRows {
				err = fmt.Errorf("Couldn't find log event with id %d", id)
			}
			return nil, err
		}
	} else {
		ds := data_store.New()
		c, err := ds.GetLogInfo(id)
		if err != nil {
			return nil, err
		}
		if c != nil {
			le = c.(*LogInfo)
			le.Id = id
		}
	}
	return le, nil
}

func (le *LogInfo)Delete() error {
	if config.Config.UseMySQL {
		return le.deleteMySQL()
	} else {
		ds := data_store.New()
		ds.DeleteLogInfo(le.Id)
	}
	return nil
}

func PurgeLogInfos(id int) (int64, error) {
	if config.Config.UseMySQL {
		return purgeMySQL(id)
	} else {
		ds := data_store.New()
		return ds.PurgeLogInfoBefore(id)
	}
}


// Get a slice of the logged events. May be called with an offset and limit, 
// (in that order) but that is not required. The offset can be specified without
// a limit, but a limit requires an offset (which can be 0).
func GetLogInfos(limits ...int) []*LogInfo {
	if config.Config.UseMySQL {
		return getLogInfoListMySQL(limits...)
	} else {
		var offset, limit int
		if len(limits) > 0 {
			offset = limits[0]
			if len(limits) > 1 {
				limit = limits[1]
			}
		} else {
			offset = 0
		}
		ds := data_store.New()
		arr := ds.GetLogInfoList()
		lis := make([]*LogInfo, len(arr))
		var keys []int
		for k := range arr {
			keys = append(keys, k)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(keys)))
		n := 0
		for _, i := range keys {
			k, ok := arr[i]
			if ok {
				item := k.(*LogInfo)
				item.Id = i
				lis[n] = item
				n++
			}
		}
		if len(lis) == 0 {
			return lis
		}
		if len(limits) > 1 {
			limit = offset + limit
			if limit > len(lis) {
				limit = len(lis)
			}
		} else {
			limit = len(lis)
		}
		return lis[offset:limit]
	}
}
