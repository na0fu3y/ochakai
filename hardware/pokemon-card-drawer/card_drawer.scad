/*
 * Pokemon card drawer cabinet for the Bambu Lab A1 mini (180x180x180 mm).
 *
 * Every part is a flat plate printed lying on the build plate, so the
 * surfaces that touch cards get the build-plate finish (no layer ridges).
 * Plates join with friction-fit tabs and slots; the only glued joint is
 * the display frame onto the drawer's inner front plate.
 *
 * Units: mm.  Cards: 63x88 raw; the pocket clears double-sleeved cards
 * (about 70x95).
 *
 * Select the part to export with -D 'part="..."' (see Makefile), or set
 * `part` below.  "assembly" shows everything put together for preview.
 */

/* [Part selection] */
// assembly | drawer_bottom | drawer_side_left | drawer_side_right | drawer_back | drawer_front_inner | drawer_front_frame | window_pane | case_bottom | case_top | case_side_left | case_side_right | case_back | clip | follower | coupon_slot | coupon_tab | coupon_rail
part = "assembly";
// assembly preview only: how far the drawer is pulled out
pull = 0;

/* [Fit] */
// tab-to-slot clearance per side; print the coupons first to tune
fit = 0.15;
// drawer-to-case sliding clearance per side
slide = 0.5;

/* [Plates] */
// wall plate thickness
t = 2.0;
// case top/bottom plate thickness
tb = 2.8;

/* [Card space] */
// drawer inner width; double-sleeved cards (~70) plus finger room
pocket_w = 74;
// headroom above the drawer floor; double-sleeved height (~95) plus play
card_space = 96;
// length of the card row (sets capacity)
row_len = 150.5;
// drawer side/back wall height
wall_h = 45;

/* [Display pocket] */
// slot depth for pane + label card
disp_gap = 2.4;
pane_t = 1.2;
pane_w = 69;
pane_h = 92;
// window cutout in the frame
win_w = 55;
win_h = 78;
rib_w = 3.5;
// finger notch in the frame top edge
notch_w = 20;
notch_d = 9.6;

/* [Tabs] */
tab_l = 20;

/* [Stacking] */
nub = 12;
nub_h = 1.2;
rec_d = 1.4;

/* [Rails and clips] */
rail_base = 7;
rail_top = 9.5;
rail_h = 1.8;
rail_z0 = 10;
clip_leg = 42;
clip_bridge = 8;

/* [Hidden] */
// ---- derived: drawer ----
dbw = pocket_w + 2 * t;        // drawer bottom width (78)
dbl = row_len + 2 * t + 1.5;   // drawer bottom length (156): 1 front lip + 0.5 rear margin
side_y0 = 3;                   // drawer-local y where the side walls start
side_len = dbl - side_y0;      // 153
back_c = side_y0 + row_len + t / 2;   // back wall center y (154.5)
// ---- derived: case ----
ciw = dbw + 2 * slide;         // case inner width (79)
cow = ciw + 2 * t;             // case outer width (83)
cih = t + card_space + 1;      // case inner height (99)
coh = cih + 2 * tb;            // case outer height (104.6)
case_front = -disp_gap;        // global y of the case front edge (-2.4)
back_face = dbl - 1 + 0.4;     // global y of the case back wall inner face (155.4)
csl = back_face + t + 0.8 - case_front;  // case plate/side length (160.6)
back_lc = back_face + t / 2 - case_front; // back wall slot center, case-local (158.8)
side_tab_x = [25, 80, 135];    // case side tab centers, case-local
nub_x = 29.5;                  // nub center offset from case midline
nub_y = [12.4, 147.4];         // nub centers, case-local
rail_x = [27.4, 134.4];        // rail centers, case-local
clip_t = 2 * rail_h - 0.3;     // clip thickness: inter-case gap minus play (3.3)
fw = cow;                      // frame width
fh = coh - 0.2;                // frame height, shy so stacked frames don't bind

// ---- helpers ----

// slot for a tab of length l running along Y, wall thickness w
module slot_v(cx, cy, w = t, l = tab_l) {
    translate([cx - w / 2 - fit, cy - l / 2 - fit])
        square([w + 2 * fit, l + 2 * fit]);
}

// slot for a tab of length l running along X
module slot_h(cx, cy, l = tab_l, w = t) {
    translate([cx - l / 2 - fit, cy - w / 2 - fit])
        square([l + 2 * fit, w + 2 * fit]);
}

// tapered stacking nub, centered at origin, rising from z=0
module stack_nub() {
    hull() {
        translate([-nub / 2, -nub / 2, 0]) cube([nub, nub, 0.01]);
        translate([-(nub - 1.6) / 2, -(nub - 1.6) / 2, nub_h - 0.01])
            cube([nub - 1.6, nub - 1.6, 0.01]);
    }
}

// dovetail rail on a case side, base on z=zb, centered on x=xc
module rail(xc, zb) {
    hull() {
        translate([xc - rail_base / 2, rail_z0, zb])
            cube([rail_base, cih - rail_z0, 0.01]);
        translate([xc - rail_top / 2, rail_z0, zb + rail_h - 0.01])
            cube([rail_top, cih - rail_z0, 0.01]);
    }
}

// ---- drawer parts (modeled in print orientation, card-facing side at z=0) ----

module drawer_bottom() {
    linear_extrude(t) difference() {
        translate([-dbw / 2, 0]) square([dbw, dbl]);
        for (sx = [-1, 1], yc = [26, 79.5, 133])
            slot_v(sx * (pocket_w + t) / 2, yc);
        for (sx = [-1, 1]) slot_h(sx * 20, back_c);
        for (sx = [-1, 1]) slot_h(sx * 20, 2);
    }
}

// left side as printed; the right side is its mirror
module drawer_side() {
    linear_extrude(t) difference() {
        union() {
            square([side_len, wall_h]);
            for (xc = [23, 76.5, 130])
                translate([xc - tab_l / 2, -t]) square([tab_l, t]);
        }
        // slot for the back wall's side tab
        translate([back_c - side_y0 - t / 2 - fit, 20 - tab_l / 2 - fit])
            square([t + 2 * fit, tab_l + 2 * fit]);
    }
}

module drawer_back() {
    linear_extrude(t) union() {
        translate([-pocket_w / 2, 0]) square([pocket_w, wall_h]);
        for (sx = [-1, 1])
            translate([sx * 20 - tab_l / 2, -t]) square([tab_l, t]);
        for (sx = [-1, 1])
            translate([sx == 1 ? pocket_w / 2 : -pocket_w / 2 - t, 20 - tab_l / 2])
                square([t, tab_l]);
    }
}

module drawer_front_inner() {
    linear_extrude(t) union() {
        translate([-dbw / 2, 0]) square([dbw, card_space]);
        for (sx = [-1, 1])
            translate([sx * 20 - tab_l / 2, -t]) square([tab_l, t]);
    }
}

// display frame, printed face down; ribs rise toward the viewer's back side
module drawer_front_frame() {
    linear_extrude(t) difference() {
        translate([-fw / 2, 0]) square([fw, fh]);
        // window; card sits at y 8.8..96.8 behind it
        translate([-win_w / 2, 13.8]) square([win_w, win_h]);
        translate([-notch_w / 2, fh - notch_d]) square([notch_w, notch_d + 1]);
    }
    // ribs land on the inner front plate (width dbw, from the floor up)
    for (sx = [-1, 1])
        translate([sx == 1 ? dbw / 2 - rib_w : -dbw / 2, tb + t, t])
            cube([rib_w, card_space, disp_gap]);
    translate([-(dbw / 2 - rib_w), tb + t, t])
        cube([dbw - 2 * rib_w, 4, disp_gap]);
}

module window_pane() {
    translate([-pane_w / 2, 0]) cube([pane_w, pane_h, pane_t]);
}

// ---- case parts ----

module case_plate_2d() {
    difference() {
        translate([-cow / 2, 0]) square([cow, csl]);
        for (sx = [-1, 1], yc = side_tab_x)
            slot_v(sx * (ciw + t) / 2, yc);
        for (sx = [-1, 1]) slot_h(sx * 20, back_lc);
    }
}

// printed with the drawer-facing side down (z=0)
module case_bottom() {
    difference() {
        linear_extrude(tb) case_plate_2d();
        // stacking recesses, on the underside in use
        for (sx = [-1, 1], yc = nub_y)
            translate([sx * nub_x - (nub + 1) / 2, yc - (nub + 1) / 2, tb - rec_d])
                cube([nub + 1, nub + 1, rec_d + 1]);
    }
}

module case_top() {
    linear_extrude(tb) case_plate_2d();
    for (sx = [-1, 1], yc = nub_y)
        translate([sx * nub_x, yc, tb]) stack_nub();
}

// left side as printed, exterior face (rails) up; right is its mirror
module case_side() {
    linear_extrude(t) difference() {
        union() {
            square([csl, cih]);
            for (xc = side_tab_x) {
                translate([xc - tab_l / 2, -tb]) square([tab_l, tb]);
                translate([xc - tab_l / 2, cih]) square([tab_l, tb]);
            }
        }
        for (yc = [30, 70])
            translate([back_lc - t / 2 - fit, yc - tab_l / 2 - fit])
                square([t + 2 * fit, tab_l + 2 * fit]);
    }
    for (xc = rail_x) rail(xc, t);
}

module case_back() {
    linear_extrude(t) union() {
        translate([-ciw / 2, 0]) square([ciw, cih]);
        for (sx = [-1, 1]) {
            translate([sx * 20 - tab_l / 2, -tb]) square([tab_l, tb]);
            translate([sx * 20 - tab_l / 2, cih]) square([tab_l, tb]);
        }
        for (sx = [-1, 1], yc = [30, 70])
            translate([sx == 1 ? ciw / 2 : -ciw / 2 - t, yc - tab_l / 2])
                square([t, tab_l]);
    }
}

// ---- connectors ----

// staple clip joining two adjacent cases; slides down over a rail pair.
// Printed flat; X = leg length, Y = across the rails, Z = thickness.
module clip_half() {
    mid = clip_t / 2;
    inner0 = rail_base / 2 + 0.25 + fit;   // leg mouth at the case walls
    inner1 = rail_top / 2 + 0.25;          // leg waist at the gap middle
    outer = rail_top / 2 + 6;
    for (seg = [[0, inner0, mid, inner1], [mid, inner1, clip_t, inner0]])
        hull() {
            translate([0, seg[1], seg[0]]) cube([clip_leg, outer - seg[1], 0.01]);
            translate([0, seg[3], seg[2] - 0.01]) cube([clip_leg, outer - seg[3], 0.01]);
        }
}

module clip() {
    outer = rail_top / 2 + 6;
    clip_half();
    mirror([0, 1, 0]) clip_half();
    translate([clip_leg, -outer, 0]) cube([clip_bridge, 2 * outer, clip_t]);
}

// L-shaped follower to keep a part-filled drawer's cards upright
module follower() {
    cube([pocket_w - 1, 80, t]);
    cube([pocket_w - 1, t, 28]);
}

// ---- fit-test coupons ----

module coupon_slot() {
    linear_extrude(t) difference() {
        translate([-20, -12]) square([40, 24]);
        slot_h(0, 0);
    }
}

module coupon_tab() {
    linear_extrude(t) union() {
        translate([-12, 0]) square([24, 14]);
        translate([-tab_l / 2, -t]) square([tab_l, t]);
    }
}

// print two, hold them rail-to-rail, and try the clip over the pair
module coupon_rail() {
    cube([24, 40, t]);
    hull() {
        translate([12 - rail_base / 2, 5, t]) cube([rail_base, 30, 0.01]);
        translate([12 - rail_top / 2, 5, t + rail_h - 0.01]) cube([rail_top, 30, 0.01]);
    }
}

// ---- assembly preview ----

module drawer_assembly() {
    color("lightsteelblue") {
        // bottom: card face up at z=t, local y - 1 = global y
        translate([0, -1, t]) mirror([0, 0, 1]) drawer_bottom();
        // left side: interior face at x=-pocket_w/2
        translate([-pocket_w / 2, side_y0 - 1, t])
            rotate([90, 0, 90]) mirror([0, 0, 1]) drawer_side();
        // right side
        mirror([1, 0, 0]) translate([-pocket_w / 2, side_y0 - 1, t])
            rotate([90, 0, 90]) mirror([0, 0, 1]) drawer_side();
        // back wall, interior face toward the front
        translate([0, back_c - 1 + t / 2, t]) rotate([90, 0, 0]) drawer_back();
        // inner front plate, interior face toward the back
        translate([0, 0, t]) rotate([90, 0, 0]) mirror([0, 0, 1]) drawer_front_inner();
    }
    color("white")
        translate([0, -disp_gap - t, -tb]) mirror([0, 0, 1]) rotate([-90, 0, 0])
            drawer_front_frame();
    color("lightcyan", 0.5)
        translate([0, -disp_gap, t + 4]) rotate([90, 0, 0]) mirror([0, 0, 1])
            window_pane();
}

module case_assembly() {
    color("gainsboro") {
        translate([0, case_front, 0]) mirror([0, 0, 1]) case_bottom();
        translate([0, case_front, cih]) case_top();
        // right side, interior at x=ciw/2, rails outward
        translate([ciw / 2, case_front, 0]) rotate([90, 0, 90]) case_side();
        mirror([1, 0, 0]) translate([ciw / 2, case_front, 0])
            rotate([90, 0, 90]) case_side();
        translate([0, back_face + t, 0]) rotate([90, 0, 0]) case_back();
    }
}

module assembly() {
    case_assembly();
    translate([0, pull, 0]) drawer_assembly();
}

// ---- part selection ----

if (part == "assembly") assembly();
else if (part == "drawer_bottom") drawer_bottom();
else if (part == "drawer_side_left") mirror([1, 0, 0]) drawer_side();
else if (part == "drawer_side_right") drawer_side();
else if (part == "drawer_back") drawer_back();
else if (part == "drawer_front_inner") drawer_front_inner();
else if (part == "drawer_front_frame") drawer_front_frame();
else if (part == "window_pane") window_pane();
else if (part == "case_bottom") case_bottom();
else if (part == "case_top") case_top();
else if (part == "case_side_left") mirror([1, 0, 0]) case_side();
else if (part == "case_side_right") case_side();
else if (part == "case_back") case_back();
else if (part == "clip") clip();
else if (part == "follower") follower();
else if (part == "coupon_slot") coupon_slot();
else if (part == "coupon_tab") coupon_tab();
else if (part == "coupon_rail") coupon_rail();
