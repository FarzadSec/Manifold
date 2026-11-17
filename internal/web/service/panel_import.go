package service

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/importer"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/random"
)

const importKeep = 30 * time.Minute

// PanelImportService moves users from another panel: read, preview, then create.
type PanelImportService struct{}

// ImportUserPreview is one user of a read source, as the preview lists it.
type ImportUserPreview struct {
	Username  string   `json:"username" example:"alice"`
	Enabled   bool     `json:"enabled" example:"true"`
	DataLimit int64    `json:"dataLimit" example:"53687091200"`
	Used      int64    `json:"used" example:"1073741824"`
	Expire    int64    `json:"expire" example:"1771116000000"`
	Protocols []string `json:"protocols" example:"[\"vless\",\"trojan\"]"`
	Exists    bool     `json:"exists" example:"false"`
}

// ImportPreview is what a source holds, kept for importKeep under Id.
type ImportPreview struct {
	Id        string              `json:"id" example:"f3a1c0d9e8b7a6c5"`
	Source    string              `json:"source" example:"marzban"`
	Users     int                 `json:"users" example:"120"`
	Existing  int                 `json:"existing" example:"0"`
	Protocols map[string]int      `json:"protocols"`
	HasSecret bool                `json:"hasSecret" example:"true"`
	Warnings  []string            `json:"warnings"`
	Sample    []ImportUserPreview `json:"sample"`
}

// ImportApplyRequest creates the users of a preview on the inbounds picked per protocol.
type ImportApplyRequest struct {
	Id       string           `json:"id" example:"f3a1c0d9e8b7a6c5"`
	Inbounds map[string][]int `json:"inbounds"`
	// Fallback takes the users that have no credential for any picked protocol.
	Fallback  []int  `json:"fallback" example:"[]"`
	KeepLinks bool   `json:"keepLinks" example:"true"`
	OwnerId   int    `json:"ownerId" example:"0"`
	Secret    string `json:"secret,omitempty"`
}

// ImportApplyResult is what an import created.
type ImportApplyResult struct {
	Created int                `json:"created" example:"118"`
	Skipped []BulkCreateReport `json:"skipped"`
	Aliases int                `json:"aliases" example:"118"`
}

var imports = struct {
	sync.Mutex
	held map[string]heldImport
}{held: map[string]heldImport{}}

type heldImport struct {
	res *importer.Result
	at  time.Time
}

func holdImport(res *importer.Result) string {
	imports.Lock()
	defer imports.Unlock()
	for id, h := range imports.held {
		if time.Since(h.at) > importKeep {
			delete(imports.held, id)
		}
	}
	id := random.Seq(16)
	imports.held[id] = heldImport{res: res, at: time.Now()}
	return id
}

func heldResult(id string) (*importer.Result, error) {
	imports.Lock()
	defer imports.Unlock()
	h, ok := imports.held[id]
	if !ok || time.Since(h.at) > importKeep {
		return nil, common.NewError("this preview has expired; read the source again")
	}
	return h.res, nil
}

// Preview holds a read source and describes it.
func (s *PanelImportService) Preview(res *importer.Result) (ImportPreview, error) {
	if res == nil || len(res.Users) == 0 {
		return ImportPreview{}, common.NewError("the source has no users")
	}
	var existing []string
	names := make([]string, 0, len(res.Users))
	for _, u := range res.Users {
		names = append(names, u.Username)
	}
	for _, batch := range chunkStrings(names, sqlInChunk) {
		var found []string
		database.GetDB().Model(&model.ClientRecord{}).Where("email IN ?", batch).Pluck("email", &found)
		existing = append(existing, found...)
	}
	p := ImportPreview{
		Id: holdImport(res), Source: res.Source, Users: len(res.Users), Existing: len(existing),
		Protocols: res.Protocols(), HasSecret: res.TokenSecret != "", Warnings: res.Warnings, Sample: []ImportUserPreview{},
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	for _, u := range res.Users[:min(len(res.Users), 50)] {
		protos := make([]string, 0, len(u.Creds))
		for k := range u.Creds {
			protos = append(protos, k)
		}
		sort.Strings(protos)
		p.Sample = append(p.Sample, ImportUserPreview{
			Username: u.Username, Enabled: u.Enabled, DataLimit: u.DataLimit, Used: u.Up + u.Down,
			Expire: u.Expire, Protocols: protos, Exists: slices.Contains(existing, u.Username),
		})
	}
	return p, nil
}

// ReadPanelFile reads an uploaded database or backup of another panel.
func ReadPanelFile(ctx context.Context, path string, raw []byte) (*importer.Result, error) {
	if len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') {
		return importer.ReadHiddify(raw)
	}
	return importer.ReadSQLite(ctx, path)
}

func clientFor(u importer.User, req ImportApplyRequest, protoOf map[int]string) (ClientCreatePayload, bool) {
	var ids []int
	for proto, list := range req.Inbounds {
		if _, ok := u.Creds[proto]; ok {
			ids = append(ids, list...)
		}
	}
	if len(ids) == 0 {
		ids = append(ids, req.Fallback...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	targets := map[string]bool{}
	for _, id := range ids {
		targets[protoOf[id]] = true
	}
	c := model.Client{
		Email: u.Username, Enable: u.Enabled, TotalGB: u.DataLimit, ExpiryTime: u.Expire,
		TgID: u.TgID, Comment: u.Note, LimitIP: u.LimitIP, TrafficReset: u.Reset,
	}
	for _, p := range []string{importer.VLESS, importer.VMess} {
		if cred, ok := u.Creds[p]; ok && cred.ID != "" && c.ID == "" {
			c.ID = cred.ID
		}
	}
	if cred, ok := u.Creds[importer.VLESS]; ok && targets["vless"] {
		c.Flow = cred.Flow
	}
	for _, p := range []string{importer.Trojan, importer.Shadowsocks, importer.Hysteria2} {
		if cred, ok := u.Creds[p]; ok && cred.Password != "" && c.Password == "" {
			c.Password = cred.Password
		}
	}
	if cred, ok := u.Creds[importer.Hysteria2]; ok {
		c.Auth = cred.Password
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.TrafficReset == "" {
		c.TrafficReset = "never"
	}
	c.SubID = random.Seq(16)
	if req.KeepLinks && u.SubID != "" {
		c.SubID = u.SubID
	}
	return ClientCreatePayload{
		Client: c, InboundIds: ids, OwnerId: req.OwnerId,
		Traffic: &ClientPortableTraffic{Up: u.Up, Down: u.Down},
	}, u.SubID != "" && req.KeepLinks
}

// Apply creates a preview's users. Users whose name is taken are skipped; with KeepLinks
// the old panel's links are stored as aliases of each new user's subscription.
func (s *PanelImportService) Apply(req ImportApplyRequest) (ImportApplyResult, error) {
	res, err := heldResult(req.Id)
	if err != nil {
		return ImportApplyResult{}, err
	}
	protoOf := map[int]string{}
	for proto, ids := range req.Inbounds {
		if proto != importer.VLESS && proto != importer.VMess && proto != importer.Trojan &&
			proto != importer.Shadowsocks && proto != importer.Hysteria2 {
			return ImportApplyResult{}, common.NewError("unknown protocol", proto)
		}
		for _, id := range ids {
			var ib model.Inbound
			if err := database.GetDB().Select("id, protocol").First(&ib, id).Error; err != nil {
				return ImportApplyResult{}, common.NewError("no inbound", id)
			}
			want := proto
			if want == importer.Hysteria2 {
				want = "hysteria"
			}
			if !strings.HasPrefix(string(ib.Protocol), want) {
				return ImportApplyResult{}, common.NewError("inbound", id, "is", ib.Protocol, "not", proto)
			}
			protoOf[id] = string(ib.Protocol)
		}
	}
	for _, id := range req.Fallback {
		var ib model.Inbound
		if err := database.GetDB().Select("id, protocol").First(&ib, id).Error; err != nil {
			return ImportApplyResult{}, common.NewError("no inbound", id)
		}
		protoOf[id] = string(ib.Protocol)
	}
	items := make([]ClientCreatePayload, 0, len(res.Users))
	for _, u := range res.Users {
		item, _ := clientFor(u, req, protoOf)
		items = append(items, item)
	}
	out, needRestart, err := (&ClientService{}).ImportClients(&InboundService{}, items)
	if err != nil {
		return ImportApplyResult{}, err
	}
	if needRestart {
		(&XrayService{}).SetToNeedRestart()
	}
	result := ImportApplyResult{Created: out.Created, Skipped: out.Skipped}
	if result.Skipped == nil {
		result.Skipped = []BulkCreateReport{}
	}
	if !req.KeepLinks {
		return result, nil
	}
	skipped := map[string]bool{}
	for _, r := range out.Skipped {
		skipped[r.Email] = true
	}
	secret := strings.TrimSpace(req.Secret)
	if secret == "" {
		secret = res.TokenSecret
	}
	var aliases []model.SubAlias
	for i, u := range res.Users {
		if skipped[u.Username] {
			continue
		}
		subID := items[i].Client.SubID
		for _, a := range u.Aliases {
			if a != "" && a != subID {
				aliases = append(aliases, model.SubAlias{Alias: a, SubId: subID, Source: res.Source})
			}
		}
		if u.TokenName != "" && secret != "" {
			aliases = append(aliases, model.SubAlias{Alias: "token:" + u.TokenName, SubId: subID, Source: res.Source, NotBefore: u.RevokedAt})
		}
	}
	db := database.GetDB()
	if len(aliases) > 0 {
		tx := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&aliases, 200)
		if tx.Error != nil {
			return result, tx.Error
		}
		result.Aliases = int(tx.RowsAffected)
	}
	if secret != "" {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.LegacySubSecret{Source: res.Source, Secret: secret}).Error; err != nil {
			return result, err
		}
	}
	SubAliasesChanged()
	return result, nil
}

// SubAliasesChanged makes the next subscription request look for aliases again.
func SubAliasesChanged() { aliasState.Store(0) }

// aliasState caches whether any alias or secret exists: 0 unknown, 1 none, 2 some.
var aliasState atomic.Int32

var errNoAlias = errors.New("no alias")

// ResolveSubAlias turns a link another panel handed out into the user's subscription id.
// An id a user holds is returned as it is; ok reports that an alias matched.
func ResolveSubAlias(id string) (string, bool) {
	if id == "" || aliasState.Load() == 1 {
		return id, false
	}
	db := database.GetDB()
	if aliasState.Load() == 0 {
		var a, s int64
		db.Model(&model.SubAlias{}).Count(&a)
		db.Model(&model.LegacySubSecret{}).Count(&s)
		if a+s == 0 {
			aliasState.Store(1)
			return id, false
		}
		aliasState.Store(2)
	}
	var held []string
	db.Model(&model.ClientRecord{}).Where("sub_id = ?", id).Limit(1).Pluck("sub_id", &held)
	if len(held) > 0 {
		return id, false
	}
	if !strings.HasPrefix(id, "token:") {
		if sub, err := aliasTo(id, 0); err == nil {
			return sub, true
		}
	}
	var secrets []model.LegacySubSecret
	db.Find(&secrets)
	for _, s := range secrets {
		if name, at, ok := importer.ParseMarzbanToken(id, s.Secret); ok {
			if sub, err := aliasTo("token:"+name, at.UnixMilli()); err == nil {
				return sub, true
			}
		}
	}
	return id, false
}

func aliasTo(alias string, madeAt int64) (string, error) {
	var a model.SubAlias
	if err := database.GetDB().Where("alias = ?", alias).First(&a).Error; err != nil {
		return "", err
	}
	if madeAt > 0 && a.NotBefore > 0 && madeAt < a.NotBefore {
		return "", errNoAlias
	}
	return a.SubId, nil
}
