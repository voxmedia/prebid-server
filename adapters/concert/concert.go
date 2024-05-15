package concert

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v2/adapters"
	"github.com/prebid/prebid-server/v2/config"
	"github.com/prebid/prebid-server/v2/errortypes"
	"github.com/prebid/prebid-server/v2/openrtb_ext"
)

type adapter struct {
	endpoint string
}

const adapterVersion = "1.0.0"

// Builder builds a new instance of the Concert adapter for the given bidder with the given config.
func Builder(bidderName openrtb_ext.BidderName, config config.Adapter, server config.Server) (adapters.Bidder, error) {
	bidder := &adapter{
		endpoint: config.Endpoint,
	}
	return bidder, nil
}

func (a *adapter) MakeRequests(request *openrtb2.BidRequest, requestInfo *adapters.ExtraRequestInfo) ([]*adapters.RequestData, []error) {
	bidderImpExt, err := getBidderExt(request.Imp[0])
	if err != nil {
		return nil, []error{fmt.Errorf("get bidder ext: %v", err)}
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, []error{err}
	}

	var requestMap map[string]interface{}
	err = json.Unmarshal(requestJSON, &requestMap)
	if err != nil {
		return nil, []error{err}
	}

	if requestMap["ext"] == nil {
		requestMap["ext"] = make(map[string]interface{})
	}
	requestMap["ext"].(map[string]interface{})["adapterVersion"] = adapterVersion
	requestMap["ext"].(map[string]interface{})["partnerId"] = bidderImpExt.PartnerId

	requestJSON, err = json.Marshal(requestMap)
	if err != nil {
		return nil, []error{err}
	}

	headers := http.Header{}
	headers.Add("Content-Type", "application/json")
	headers.Add("Accept", "application/json")
	requestData := &adapters.RequestData{
		Method:  "POST",
		Uri:     a.endpoint,
		Body:    requestJSON,
		Headers: headers,
		ImpIDs:  openrtb_ext.GetImpIDs(request.Imp),
	}

	return []*adapters.RequestData{requestData}, nil
}

func (a *adapter) MakeBids(request *openrtb2.BidRequest, requestData *adapters.RequestData, responseData *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	if responseData.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if responseData.StatusCode == http.StatusBadRequest {
		err := &errortypes.BadInput{
			Message: "Unexpected status code: 400. Bad request from publisher. Run with request.debug = 1 for more info.",
		}
		return nil, []error{err}
	}

	if responseData.StatusCode != http.StatusOK {
		err := &errortypes.BadServerResponse{
			Message: fmt.Sprintf("Unexpected status code: %d. Run with request.debug = 1 for more info.", responseData.StatusCode),
		}
		return nil, []error{err}
	}

	var response openrtb2.BidResponse
	if err := json.Unmarshal(responseData.Body, &response); err != nil {
		return nil, []error{err}
	}

	bidResponse := adapters.NewBidderResponseWithBidsCapacity(len(request.Imp))
	bidResponse.Currency = response.Cur
	var errors []error
	for _, seatBid := range response.SeatBid {
		for i, bid := range seatBid.Bid {
			bidType, err := getMediaTypeForBid(bid)
			if err != nil {
				errors = append(errors, err)
				continue
			}
			bidResponse.Bids = append(bidResponse.Bids, &adapters.TypedBid{
				Bid:     &seatBid.Bid[i],
				BidType: bidType,
			})
		}
	}

	if len(errors) > 0 {
		return nil, errors
	}

	if len(bidResponse.Bids) == 0 {
		return nil, []error{fmt.Errorf("no bids returned")}
	}

	return bidResponse, nil
}

func getMediaTypeForBidFromExt(bid openrtb2.Bid) (openrtb_ext.BidType, error) {
	if bid.Ext != nil {
		var bidExt openrtb_ext.ExtBid
		err := json.Unmarshal(bid.Ext, &bidExt)
		if err == nil && bidExt.Prebid != nil {
			return openrtb_ext.ParseBidType(string(bidExt.Prebid.Type))
		}
	}

	return "", &errortypes.BadServerResponse{
		Message: fmt.Sprintf("Failed to parse media type for bid: \"%s\"", bid.ImpID),
	}
}

func getMediaTypeForBid(bid openrtb2.Bid) (openrtb_ext.BidType, error) {
	switch bid.MType {
	case openrtb2.MarkupBanner:
		return openrtb_ext.BidTypeBanner, nil
	case openrtb2.MarkupVideo:
		return openrtb_ext.BidTypeVideo, nil
	case openrtb2.MarkupAudio:
		return openrtb_ext.BidTypeAudio, nil
	case openrtb2.MarkupNative:
		return "", fmt.Errorf("native media types are not yet supported")
	default:
		return getMediaTypeForBidFromExt(bid)
	}
}

func getBidderExt(imp openrtb2.Imp) (bidderImpExt openrtb_ext.ImpExtConcert, err error) {
	var impExt adapters.ExtImpBidder
	if err = json.Unmarshal(imp.Ext, &impExt); err != nil {
		return bidderImpExt, fmt.Errorf("imp ext: %v", err)
	}
	if err = json.Unmarshal(impExt.Bidder, &bidderImpExt); err != nil {
		return bidderImpExt, fmt.Errorf("bidder ext: %v", err)
	}
	return bidderImpExt, nil
}
