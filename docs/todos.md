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
18. Done. The tidal charts have weird corners where the border ends.  Maybe there is some sort of border radius that is causing that error. Let's remove the border entirely. 
19. Done. I'd like the voyage report to include the wind charts from each stop from the destination briefing. 
20. Done. In print view. Look at /Users/tpryan/Documents/GitHub/navalplan/temp/Screenshot 2026-04-19 at 12.35.50 PM.png. The map doesn't fill it's container entirely. Can we change that? Make it fill the entire rounded container. 
21. Done. In print view. Can the sailing season grid be marked that it can't be page broken? I would like to avoid this look: /Users/tpryan/Documents/GitHub/navalplan/temp/Screenshot 2026-04-19 at 12.38.32 PM.png
22. Done. In print view. Can we hide any refs or links since presumably these will be printed and can't be followed.    
23. Done. In print view, Can charter info and airport info tile horizontally. So it doesn't look like /Users/tpryan/Documents/GitHub/navalplan/temp/Screenshot 2026-04-19 at 12.40.51 PM.png
24. Done. In print view, can we avoid breaking in the middle of major hub items, like we do in this pic: /Users/tpryan/Documents/GitHub/navalplan/temp/Screenshot 2026-04-19 at 12.41.59 PM.png
25. Done. YThe Safety overview is not showing up in the print view of the voyage report.  We need to fix that. 
26. Done. I need to simplify and improve the behavior of the wind graph in each stop. Same data, better view. 
27. Done. I need to improve the mobile version of the app to make it actually useful.
    * Navigation has to be improved.
    * Discover interface has to work. 
28. Done. I'd like 503 from gemini to be reported to the frontend so the user knows the model is busy. Probably in the same place as the backend and agent connection problems, but in a warning style instead of an error. 
29. Done. The travel times table is awesome, but it doesn't really need to be calculated by an agent, can we write deterministic code that will handle it? 
30. Done. Let's add a method for use by an hourly scheduled task that updates the weather every hour and runs the alert agent every hour for trips that are happening or soon to happen (2 days.)
31. Done. When in mobile view the discovery picker should break into two 6 months rows. 
32. Done. Actually schdeule task from 30. 
33. Done. Change the order of np-weather-tile: Conditions, temp, wind, waves, sunrise, sunset. 
34. Done. make buildImprove display of precipitation in hourly_track. It should be at bottom. it should be easier to read.
35. Done. 35. In really narrow screens, the whole content spills out the viewport. I'd like it to look right even when teh viewport is <390 px.
36. Done. 36. Add GPS check-in feature for voyage owners to show current position on map and shared report.
37. Done. Back, Guide, Report, Tracks buttons aren't designed well. They words needs to be smaller fonts. or somehow better spaced. 
38. Done. The dropdown for Track Type should be styled like the app, instead of native. 
39. Done. Delete uploaded tracks button does not work. (Actually it's a z index issue, the confirm modal needs to be higher.)
40. Done. Can we trigger a debrief job for all tracks at once instead of one by one?
41. Done. Can we add the debrief content to the voyage report?
42. Done. Debriefing step is not correct.  It should compare the actual to the planned, and come up with conclusions. so they should be paired between planned actual.
43. Done. When adding debriefing to a page of the report, can you add it along with the starting stop for each leg. I do not want a consolidated report at the end. I want one per stop. It should appear as part of the filtered sailing version of the report. 
44. Done. When we have a planned route, can we not show the direct lines between stops as they are just straght lines between two points.
45. Done. After rerunning stop research the tracks on the map disappear, it should still be there. 
46. Done. There should only be one button for debriefing. Not one per gpx track. Debrief all at once and only do all at once.
47. Done. Place debrief reports with the starting stop, not the ending stop in the report. 
48. Done. Please alter the lookout agent to also take in and use the planned routes if they exist to tailor alterts.
49. The title for debriefs in the ui should match the title of the stops, and not the title information from the uploaded tracks.  