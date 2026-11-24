# arrived-bar-phone-ellipsis

Cited as `V0.arrived-bar-phone-ellipsis`.

On the phone the arrived bar (V0.arrived-from) stacks its two lines in a column. With `align-items: baseline` each
line kept the width of its text, so a long thread title never shrank to an ellipsis and ran under the Back and ×
buttons. In the phone's column the lines now stretch to the bar's width (`align-items: stretch`, `min-width: 0`), so
the `.ell` ellipsis applies and the buttons stay clear. The desktop row is unchanged.
