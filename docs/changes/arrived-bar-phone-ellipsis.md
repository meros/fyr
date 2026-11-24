# V0.arrived-bar-phone-ellipsis: the phone's arrived bar cuts a long title with an ellipsis instead of running under Back

→ Amends V0.arrived-from. On the phone the bar stacks its two lines in a column. With `align-items: baseline` each line kept the width of its text, so a long thread title never shrank to an ellipsis and ran under the Back and × buttons. In the phone's column the lines now stretch to the bar's width (`align-items: stretch`, `min-width: 0`), so the `.ell` ellipsis applies and the buttons stay clear. The desktop row is unchanged.
