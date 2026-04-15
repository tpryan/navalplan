1.  Done. If "Itinerary Complete! Run full voyage research to get weather, tides, and pilot info for every stop." is shown then the button " Run Full Voyage Research" should not be shown. 
2.  Done. If the last stop of a trip is within 1 nautical mile of the first stop, do not bother to show "Local Facilities" for that last shop.  Maybe also indicate why they are omitted. 
3.  Done. We have multiple places where we show markers for harbors, anchorages, marinas, and other points of interest on the map. Sometimes a pointer, sometimes a blob. Can we make the colors consistent across views: anchorages:green, moorings:purple, marinas:orange. Assign colors for other ones and lets keep them consistent. 
4.  Done. Convert the markers rendered in the researcher phase - used to be the local pilot phase, to the same type of map markers rendered in the same colors as the previous todo instead of blobs. 
5.  Done. I think there are lot more possible anchorages than we are showing. Can we try and increase the number of anchorages without reducing the amounts of other points of interest.  
6. Done. For interface labeled "<!-- Discovery Intro Modal -->." It has three buttons on it. Countinue, Cancel, and Close.  Cancel and Close are redundant to each other. Close should be removed. 
7. Done. On the toolbar, the items seem unordered. The dated and undated should be grouped.  I'd like to see the dated ordered by descending by date, then by name alphabetically ascending.  Then I'd like to see the undated sorted by name ascending.    
8. Done. Tweak the Destination Guide -> Charter info to always try to include links to the chartering companies.
9. Done. Change the Map Snapshot: 
    * Markers obscure the actual stops on the map. Those markers should be smaller if possible. 
    * The map is too zoomed out to be really that useful. It would be best if the zoom matched made it so the search radius was just contained.  
10. Done. Destination Guide -> Points of interest. Links would be super helpful here. 
11. Done. Voyage Report -> Day Report -> Weather Outlook -> Getting consistent Precipitation Undefined errors.  
12. Done. Add a map with POI to the Voyage report for each stop. It should be limited to the same search radius we use for the stops. It should also be a static map.  
13. Done. Sometimes I get distance: unknown for airports in the destination guide. That should be fixed. The BVI entry currently in the database has this problem. 
14. Done. After running local pilot search the #itinerary-list still reads: "Researching area..." instead of showing the list of resources. 
15. Done. After running the stop report this message appears: Itinerary Complete! Run full voyage research to get weather, tides, and pilot info for every stop. Be we did just run the full report, so this is unnecessary
16. Done. If we can figure out ahead of time that stop 1 and 4 are the same place, we can avoid making a model heavy call that's redundant by omitting the last stop. 
17. Done. We should have better navigation on the front end.  I'd like to have deep linking to trips, and back button history rewriting so that we can navigate through the app's state using the browser. 