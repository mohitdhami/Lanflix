package api

import "net/http"

// Exported aliases used by main.go route registration.
type handler3 = func(http.ResponseWriter, *http.Request, int64, string)

var (
	GetServerInfo    = getServerInfo
	GetFile          = getFile
	PostSignup       = postSignup
	PostLogin        = postLogin
	GetGenres        = getGenres
	GetContentList   = getContentList
	GetBrowseRows    = getBrowseRows
	GetContentDetail = getContentDetail
	GetEpisodes      = getEpisodes
	StreamMovie      = streamMovie
	StreamEpisode    = streamEpisode
	SubsMovie        = subsMovie
	SubsEpisode      = subsEpisode

	GetMe         = getMe
	GetProfiles   = getProfiles
	PostProfile   = postProfile
	PutProfile    = putProfile
	DeleteProfile = deleteProfile

	PostContentView = postContentView
	GetMyList       = getMyList
	PostMyList      = postMyList
	DeleteMyList    = deleteMyList
	PostRate        = postRate
	PostProgress    = postProgress
	GetHistory      = getHistory

	GetAdminStats    = getAdminStats
	GetAdminUsers    = getAdminUsers
	PostAdminUser    = postAdminUser
	PutAdminUser     = putAdminUser
	DeleteAdminUser  = deleteAdminUser
	GetAdminContent  = getAdminContent
	PostAdminContent = postAdminContent
	PutAdminContent  = putAdminContent
	DeleteAdminContent = deleteAdminContent
	PostAdminConvert = postAdminConvert
	PostAdminEpisode = postAdminEpisode
	PutAdminEpisode = putAdminEpisode
	PostAdminEpisodeMove = postAdminEpisodeMove
	PostAdminEpisodesReorder = postAdminEpisodesReorder
	DeleteAdminEpisode = deleteAdminEpisode
	PostAdminScan    = postAdminScan
	PostAdminSeedDemo = postAdminSeedDemo
	GetAdminViews    = getAdminViews
	PostAdminPoster  = postAdminPoster
	PostAdminUpload  = postAdminUpload
	PostAdminImdbFetch = postAdminImdbFetch
	PostAdminImdbImport = postAdminImdbImport

	Authed    = authed
	AdminOnly = adminOnly
	LanIPs    = lanIPs
)
