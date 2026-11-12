"use client";
import { useEffect, useRef } from "react";

/**
 * The hero demo. Autoplays silently and loops, so it reads as part of the
 * product rather than something you press play on.
 *
 * Two things are load-bearing for that to actually happen: `muted` and
 * `playsInline`. Without muted, every browser blocks autoplay; without
 * playsInline, iOS Safari takes the video fullscreen instead of playing it in
 * place.
 */
export default function DemoVideo() {
  const ref = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const video = ref.current;
    if (!video) return;
    // React can drop the muted attribute during hydration, and an unmuted
    // video is refused autoplay — so set the property directly too.
    video.muted = true;

    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)");
    const apply = () => {
      if (reduced.matches) {
        // Motion is the thing being opted out of. Leave the poster up and give
        // them a control, rather than looping anyway.
        video.pause();
        video.controls = true;
        return;
      }
      video.controls = false;
      // Rejected autoplay is not an error worth surfacing; the poster stays.
      void video.play().catch(() => {});
    };

    apply();
    reduced.addEventListener("change", apply);
    return () => reduced.removeEventListener("change", apply);
  }, []);

  return (
    <div className="demo-frame">
      <video
        ref={ref}
        className="demo-video"
        poster="/demo-poster.jpg"
        autoPlay
        muted
        loop
        playsInline
        preload="metadata"
        disablePictureInPicture
        aria-label="A short screen recording of Envi: pulling secrets into a project from the command line"
      >
        <source src="/demo.mp4" type="video/mp4" />
      </video>
    </div>
  );
}
