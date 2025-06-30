package models

// RADIUSReply represents RADIUS reply attribute
type RADIUSReply struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// TrafficClass represents traffic classification details
type TrafficClass struct {
	OctetsIn  uint64  `json:"octets_in"`
	OctetsOut uint64  `json:"octets_out"`
	Cost      float64 `json:"cost"`
}

// RADIUSAuthorizeRequest represents authorization request
type RADIUSAuthorizeRequest struct {
	Username         string `json:"username"`
	Password         string `json:"password"`
	NASIPAddress     string `json:"nas_ip_address"`
	NASPort          string `json:"nas_port"`
	CallingStationId string `json:"calling_station_id"`
	CalledStationId  string `json:"called_station_id"`
}

// RADIUSAccountingRequest represents accounting request
type RADIUSAccountingRequest struct {
	Username          string `json:"username"`
	AcctStatusType    string `json:"acct_status_type"`
	AcctSessionId     string `json:"acct_session_id"`
	AcctInputOctets   uint64 `json:"acct_input_octets"`
	AcctOutputOctets  uint64 `json:"acct_output_octets"`
	AcctInputPackets  uint64 `json:"acct_input_packets"`
	AcctOutputPackets uint64 `json:"acct_output_packets"`
	AcctSessionTime   uint32 `json:"acct_session_time"`
	FramedIPAddress   string `json:"framed_ip_address"`
	CallingStationId  string `json:"calling_station_id"`
	NASIPAddress      string `json:"nas_ip_address"`
	NASPort           string `json:"nas_port"`
}

// BillingResult represents billing decision result
type BillingResult struct {
	Decision     string                 `json:"decision"`      // Accept/Reject
	Reason       string                 `json:"reason"`        // Rejection reason
	Amount       float64                `json:"amount"`        // Amount to charge
	Replies      []RADIUSReply          `json:"replies"`       // RADIUS replies
	PlanData     map[string]interface{} `json:"plan_data"`     // Updated plan data
	TrafficClass string                 `json:"traffic_class"` // Traffic classification
}
