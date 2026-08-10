# Renders every printable part to stl/.  Requires openscad on PATH.
PARTS = drawer_bottom drawer_side_left drawer_side_right drawer_back \
        drawer_front_inner drawer_front_frame window_pane \
        case_bottom case_top case_side_left case_side_right case_back \
        clip follower coupon_slot coupon_tab coupon_rail

STLS = $(PARTS:%=stl/%.stl)

all: $(STLS)

stl/%.stl: card_drawer.scad
	@mkdir -p stl
	openscad -o $@ -D 'part="$*"' $<

clean:
	rm -rf stl

.PHONY: all clean
