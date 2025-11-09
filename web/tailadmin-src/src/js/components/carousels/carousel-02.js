// import Swiper JS
import Swiper from "swiper";
import { Navigation, Autoplay } from "swiper/modules";
// import Swiper styles
import "swiper/css";

const swiper = new Swiper(".carouselTwo", {
  modules: [Navigation, Autoplay],
  autoplay: {
    delay: 5000,
    disableOnInteraction: false,
  },
  navigation: {
    nextEl: ".swiper-button-next",
    prevEl: ".swiper-button-prev",
  },
});
